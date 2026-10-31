#!/usr/bin/env python3
"""Package a tested Podhod main revision or receive it through the forced SSH command."""

import argparse
import datetime
import fcntl
import hashlib
import gzip
import http.client
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import time
import zlib

SERVICES = {'api': 'podhod'}
CONFIG = Path('/etc/podhod-deploy.json')
MAX_COMPRESSED = 128 * 1024 * 1024
MAX_EXPANDED = 256 * 1024 * 1024
SHA256 = re.compile(r'[a-f0-9]{64}\Z')
COMMIT = re.compile(r'[a-f0-9]{40}\Z')


class ReleaseError(RuntimeError):
    pass


def database_fingerprint(repository):
    """Gate changes to the source that owns database initialization."""
    path = repository / 'internal/storage/postgres.go'
    if not path.is_file():
        raise ReleaseError('Database source files are unavailable')
    return hashlib.sha256(path.read_bytes()).hexdigest()


def package_release(repository, binaries, destination, commit, run_number):
    actual = subprocess.check_output(['git', '-C', str(repository), 'rev-parse', 'HEAD'], text=True).strip()
    if not COMMIT.fullmatch(commit) or actual != commit or run_number < 1:
        raise ReleaseError('Bundle commit does not match checkout HEAD')
    manifest = {'schema': 1, 'commit': commit, 'run_number': run_number, 'database_sha256': database_fingerprint(repository), 'files': {}}
    for binary in SERVICES.values():
        path = binaries / binary
        data = path.read_bytes()
        if not data.startswith(b'\x7fELF') or len(data) < 64:
            raise ReleaseError('Expected Linux ELF release executables')
        manifest['files'][binary] = {'size': len(data), 'sha256': hashlib.sha256(data).hexdigest()}
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tarfile.open(destination, 'w:gz', format=tarfile.USTAR_FORMAT) as archive:
        import io
        encoded = (json.dumps(manifest, sort_keys=True) + '\n').encode()
        entry = tarfile.TarInfo('manifest.json')
        entry.size = len(encoded)
        entry.mode = 0o600
        archive.addfile(entry, io.BytesIO(encoded))
        for binary in SERVICES.values():
            archive.add(binaries / binary, arcname=binary, recursive=False)
    print(json.dumps({'commit': commit, 'database_sha256': manifest['database_sha256'], 'bundle': str(destination)}))


class BoundedInput:
    def __init__(self, stream, limit):
        self.stream = stream
        self.limit = limit
        self.received = 0

    def read(self, size=-1):
        if size < 0:
            size = self.limit + 1 - self.received
        data = self.stream.read(min(size, self.limit + 1 - self.received))
        self.received += len(data)
        if self.received > self.limit:
            raise ReleaseError('Release stream exceeds the size limit')
        return data


def receive_bundle(stream, directory, approved_database_sha256):
    received = set()
    expanded = 0
    allowed = {'manifest.json', *SERVICES.values()}
    try:
        compressed = directory / 'bundle.tar.gz'
        raw_tar = directory / 'release.tar'
        with compressed.open('xb') as output:
            shutil.copyfileobj(BoundedInput(stream, MAX_COMPRESSED), output, length=1024 * 1024)
        with gzip.open(compressed, 'rb') as decoded, raw_tar.open('xb') as output:
            shutil.copyfileobj(BoundedInput(decoded, MAX_EXPANDED + 1024 * 1024), output, length=1024 * 1024)
        # Parse only fixed ustar regular-file headers: never interpret PAX/GNU metadata.
        with raw_tar.open('rb') as archive:
            while True:
                header = archive.read(512)
                if header == b'\0' * 512:
                    if archive.read(512) != b'\0' * 512 or any(archive.read()):
                        raise ReleaseError('Release has an invalid tar trailer')
                    break
                if len(header) != 512:
                    raise ReleaseError('Release tar header is incomplete')
                member = tarfile.TarInfo.frombuf(header, 'utf-8', 'strict')
                if member.name not in allowed or member.name in received or member.type not in (tarfile.REGTYPE, tarfile.AREGTYPE):
                    raise ReleaseError('Release contains an unexpected path, duplicate or non-file entry')
                limit = 65536 if member.name == 'manifest.json' else 128 * 1024 * 1024
                expanded += member.size
                if not 0 < member.size <= limit or expanded > MAX_EXPANDED:
                    raise ReleaseError('Expanded release exceeds the size limit')
                received.add(member.name)
                remaining = member.size
                with (directory / member.name).open('xb') as output:
                    while remaining:
                        data = archive.read(min(remaining, 1024 * 1024))
                        if not data:
                            raise ReleaseError('Release file is incomplete')
                        output.write(data)
                        remaining -= len(data)
                padding = (-member.size) % 512
                if len(archive.read(padding)) != padding:
                    raise ReleaseError('Release tar padding is incomplete')
                (directory / member.name).chmod(0o600 if member.name == 'manifest.json' else 0o755)
    except (tarfile.TarError, EOFError, OSError, zlib.error) as error:
        raise ReleaseError('Release archive is invalid or incomplete') from error
    if received != allowed:
        raise ReleaseError('Release is missing required files')
    try:
        manifest = json.loads((directory / 'manifest.json').read_text())
        if (set(manifest) != {'schema', 'commit', 'run_number', 'database_sha256', 'files'} or manifest['schema'] != 1
                or type(manifest['run_number']) is not int or manifest['run_number'] < 1
                or not COMMIT.fullmatch(manifest['commit']) or not SHA256.fullmatch(manifest['database_sha256'])
                or manifest['database_sha256'] != approved_database_sha256
                or set(manifest['files']) != set(SERVICES.values())):
            raise ReleaseError('Release manifest or approved database fingerprint does not match')
        for binary in SERVICES.values():
            path = directory / binary
            metadata = manifest['files'][binary]
            if (set(metadata) != {'size', 'sha256'} or not SHA256.fullmatch(metadata['sha256'])
                    or path.stat().st_size != metadata['size']
                    or hashlib.sha256(path.read_bytes()).hexdigest() != metadata['sha256']):
                raise ReleaseError('Release executable checksum does not match')
            with path.open('rb') as executable:
                header = executable.read(64)
            if header[:6] != b'\x7fELF\x02\x01' or header[18:20] != b'\x3e\x00':
                raise ReleaseError('Release executable is not Linux amd64 ELF')
    except (ValueError, TypeError, KeyError, AttributeError) as error:
        raise ReleaseError('Release manifest is invalid') from error
    return manifest


class Deployment:
    def __init__(self, configuration):
        self.root = Path(configuration['root'])
        self.runtime_base = configuration.get('runtime_base')
        self.directory = self.root / 'deploy'
        self.override = self.directory / 'compose.images.json'
        self.state = self.directory / 'current.json'

    def docker(self, *arguments, stdout=None, timeout=180):
        try:
            result = subprocess.run(['docker', *arguments], cwd=self.root, check=True,
                                    stdout=stdout if stdout is not None else subprocess.PIPE,
                                    stderr=subprocess.PIPE, timeout=timeout,
                                    env={'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8'})
            return result.stdout
        except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
            raise ReleaseError('Docker operation failed; inspect private server diagnostics') from error

    def compose(self, *arguments, stdout=None):
        files = ['compose', '-f', str(self.root / 'compose.yaml')]
        if self.override.exists():
            files += ['-f', str(self.override)]
        return self.docker(*files, *arguments, stdout=stdout)

    def runtime_images(self):
        images = {}
        for service in SERVICES:
            container = self.compose('ps', '-q', service).decode().strip()
            if not re.fullmatch(r'[a-f0-9]{12,64}', container):
                raise ReleaseError('Expected one running container per application service')
            image = self.docker('inspect', '--format', '{{.Image}}', container).decode().strip()
            if not re.fullmatch(r'sha256:[a-f0-9]{64}', image):
                raise ReleaseError('Cannot identify the previous application image')
            images[service] = image
        return images

    def write_images(self, images):
        temporary = self.override.with_suffix('.partial')
        temporary.write_text(json.dumps({'services': {name: {'image': image} for name, image in images.items()}}) + '\n')
        temporary.chmod(0o600)
        os.replace(temporary, self.override)

    def build_images(self, release, manifest):
        if not self.runtime_base or not re.fullmatch(r'sha256:[a-f0-9]{64}', self.runtime_base):
            raise ReleaseError('The approved local runtime image is missing')
        context = release / 'image-api'
        context.mkdir()
        shutil.copyfile(release / 'podhod', context / 'podhod')
        (context / 'podhod').chmod(0o755)
        dockerfile = (f'FROM {self.runtime_base}\n'
                      'COPY --chown=10001:10001 podhod /usr/local/bin/podhod\n'
                      f'LABEL org.opencontainers.image.revision="{manifest["commit"]}"\n')
        (context / 'Dockerfile').write_text(dockerfile)
        image = f'podhod-api:{manifest["commit"]}'
        self.docker('build', '--network=none', '--pull=false', '-t', image, str(context), timeout=600)
        return {'api': image}

    def backup_databases(self, destination):
        destination.mkdir(mode=0o700)
        for database in ('podhod', 'keycloak'):
            path = destination / (database + '.dump')
            with path.open('xb') as output:
                self.compose('exec', '-T', 'db', 'pg_dump', '-U', 'postgres', '-d', database, '-Fc', stdout=output)
            path.chmod(0o600)
            with path.open('rb') as source:
                try:
                    subprocess.run(['docker', 'compose', '-f', str(self.root / 'compose.yaml'), 'exec', '-T', 'db',
                                    'pg_restore', '--list'], cwd=self.root, stdin=source,
                                   stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, check=True, timeout=60)
                except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
                    raise ReleaseError('Database backup validation failed') from error

    def check_runtime(self, expected=None):
        code, _, _ = loopback_json(8087, '/healthz')
        if code != 200:
            raise ReleaseError('API health check failed')
        configuration = {}
        for platform, client in [('ios', 'podhod-ios'), ('android', 'podhod-android')]:
            query = '' if platform == 'ios' else '?platform=android'
            code, _, value = loopback_json(8087, '/v1/config' + query)
            if code != 200 or value.get('clientID') != client:
                raise ReleaseError('Mobile identity configuration check failed')
            configuration[platform] = value
        for path in ('/v1/profile', '/mcp'):
            code, _, _ = loopback_json(8087, path)
            if code != 401:
                raise ReleaseError('API did not reject anonymous access')
        if expected is not None and configuration != expected:
            raise ReleaseError('Deployment changed the mobile identity configuration')
        return configuration

    def wait_for_runtime(self, expected):
        for attempt in range(12):
            try:
                return self.check_runtime(expected)
            except (ReleaseError, OSError, ValueError, http.client.HTTPException):
                if attempt == 11:
                    raise ReleaseError('Runtime did not pass authenticated checks within the recovery window')
                time.sleep(5)

    def activate_release(self, release, manifest):
        previous_images = self.runtime_images()
        previous_state = self.state.read_bytes() if self.state.exists() else None
        expected = self.check_runtime()
        images = self.build_images(release, manifest)
        stopped = False
        previous_handlers = {number: signal.signal(number, deployment_interrupted) for number in (signal.SIGHUP, signal.SIGTERM)}
        try:
            stopped = True
            self.compose('stop', 'api')
            stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
            self.backup_databases(self.root / 'backups' / (stamp + '-' + manifest['commit'][:12]))
            self.write_images(images)
            self.compose('config', '--quiet')
            self.compose('up', '-d', '--no-deps', 'api')
            self.wait_for_runtime(expected)
            temporary = self.state.with_suffix('.partial')
            temporary.write_text(json.dumps({'commit': manifest['commit'], 'run_number': manifest['run_number'], 'files': manifest['files'], 'images': images}) + '\n')
            temporary.chmod(0o600)
            os.replace(temporary, self.state)
        except Exception as error:
            if stopped:
                for number in previous_handlers:
                    signal.signal(number, signal.SIG_IGN)
                try:
                    self.write_images(previous_images)
                    self.compose('up', '-d', '--no-deps', 'api')
                    self.wait_for_runtime(expected)
                except Exception as recovery_error:
                    raise ReleaseError('DEPLOYMENT AND IMAGE ROLLBACK FAILED; manual recovery required') from recovery_error
                if previous_state is None:
                    self.state.unlink(missing_ok=True)
                else:
                    self.state.write_bytes(previous_state)
            raise ReleaseError('Deployment failed; previous application images restored, databases preserved') from error
        finally:
            for number, handler in previous_handlers.items():
                signal.signal(number, handler)


def deployment_interrupted(number, frame):
    raise ReleaseError('Deployment interrupted; restoring previous images')


def loopback_json(port, path, token=None, body=None, extra_headers=None):
    headers = {'Accept': 'application/json, text/event-stream'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    headers.update(extra_headers or {})
    data = None if body is None else json.dumps(body).encode()
    if data is not None:
        headers['Content-Type'] = 'application/json'
    connection = http.client.HTTPConnection('127.0.0.1', port, timeout=8)
    try:
        connection.request('GET' if body is None else 'POST', path, data, headers)
        response = connection.getresponse()
        raw = response.read(4 * 1024 * 1024 + 1)
        if len(raw) > 4 * 1024 * 1024:
            raise ReleaseError('Health response exceeds the size limit')
        if not raw:
            content = {}
        elif raw.lstrip().startswith((b'{', b'[')):
            content = json.loads(raw)
        else:
            events = [json.loads(line[5:].strip()) for line in raw.splitlines() if line.startswith(b'data:')]
            content = events[-1] if events else {}
        return response.status, {name.lower(): value for name, value in response.getheaders()}, content
    finally:
        connection.close()


def receive_release(stream):
    if os.geteuid() != 0 or CONFIG.stat().st_uid != 0 or CONFIG.stat().st_mode & 0o077:
        raise ReleaseError('Receiver requires root and a root-owned private configuration')
    command = os.environ.get('SSH_ORIGINAL_COMMAND', '')
    if command not in ('', 'status'):
        raise ReleaseError('Only the read-only status command or release stdin is accepted')
    configuration = json.loads(CONFIG.read_text())
    if not SHA256.fullmatch(configuration['approved_database_sha256']):
        raise ReleaseError('Approved database fingerprint is invalid')
    deployment = Deployment(configuration)
    if command == 'status':
        state = json.loads(deployment.state.read_text()) if deployment.state.exists() else {}
        if state:
            deployment.check_runtime()
        print(json.dumps({'commit': state.get('commit'), 'run_number': state.get('run_number', 0)}))
        return
    deployment.directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (deployment.directory / 'lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise ReleaseError('Another deployment is in progress; retry the workflow') from error
        with tempfile.TemporaryDirectory(prefix='incoming-', dir=deployment.directory) as temporary:
            release = Path(temporary)
            manifest = receive_bundle(stream, release, configuration['approved_database_sha256'])
            if deployment.state.exists():
                state = json.loads(deployment.state.read_text())
                if state['commit'] == manifest['commit']:
                    if state['files'] != manifest['files']:
                        raise ReleaseError('The already deployed commit has different executable checksums')
                    deployment.check_runtime()
                    print(json.dumps({'commit': manifest['commit'], 'status': 'already-deployed'}))
                    return
                if manifest['run_number'] <= state['run_number']:
                    raise ReleaseError('A newer workflow run is already deployed; stale release refused')
            deployment.activate_release(release, manifest)
            print(json.dumps({'commit': manifest['commit'], 'status': 'deployed'}))


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    fingerprint = commands.add_parser('database-fingerprint')
    fingerprint.add_argument('repository', type=Path)
    bundle = commands.add_parser('bundle')
    bundle.add_argument('--repository', type=Path, required=True)
    bundle.add_argument('--binaries', type=Path, required=True)
    bundle.add_argument('--output', type=Path, required=True)
    bundle.add_argument('--commit', required=True)
    bundle.add_argument('--run-number', required=True, type=int)
    commands.add_parser('receive')
    arguments = parser.parse_args()
    if arguments.command == 'database-fingerprint':
        print(database_fingerprint(arguments.repository.resolve()))
    elif arguments.command == 'bundle':
        package_release(arguments.repository.resolve(), arguments.binaries, arguments.output, arguments.commit, arguments.run_number)
    else:
        receive_release(sys.stdin.buffer)


if __name__ == '__main__':
    try:
        main()
    except (ReleaseError, OSError, ValueError, KeyError, TypeError) as error:
        print('Podhod release stopped: ' + (str(error) if isinstance(error, ReleaseError) else 'invalid configuration or local operation failed'), file=sys.stderr)
        sys.exit(1)
