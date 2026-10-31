import gzip
import hashlib
import importlib.util
import io
import json
import signal
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('podhod_release', Path(__file__).resolve().parents[1] / 'release.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)

COMMIT = 'a' * 40
DATABASE = 'd' * 64
ELF = b'\x7fELF\x02\x01' + b'\0' * 12 + b'\x3e\0' + b'\0' * 44


def release_archive(change_manifest=None, extra=None, corrupt=False):
    files = {name: ELF + name.encode() for name in release.SERVICES.values()}
    manifest = {'schema': 1, 'commit': COMMIT, 'run_number': 10, 'database_sha256': DATABASE,
                'files': {name: {'size': len(data), 'sha256': hashlib.sha256(data).hexdigest()} for name, data in files.items()}}
    if change_manifest:
        change_manifest(manifest)
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w:gz', format=tarfile.USTAR_FORMAT) as archive:
        payloads = {'manifest.json': json.dumps(manifest).encode(), **files}
        for name, data in payloads.items():
            if corrupt and name == 'podhod':
                data = data[:-1] + b'x'
            entry = tarfile.TarInfo(name)
            entry.size = len(data)
            archive.addfile(entry, io.BytesIO(data))
        if extra:
            entry, data = extra
            archive.addfile(entry, io.BytesIO(data))
    return stream.getvalue()


class ReleaseArchiveTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.directory = Path(self.scratch.name)

    def receive(self, content):
        return release.receive_bundle(io.BytesIO(content), self.directory, DATABASE)

    def test_valid_release_checks_every_binary_and_sets_executable_mode(self):
        manifest = self.receive(release_archive())
        self.assertEqual(COMMIT, manifest['commit'])
        for binary in release.SERVICES.values():
            self.assertEqual(0o755, (self.directory / binary).stat().st_mode & 0o777)

    def test_checksum_failure_does_not_create_runtime_override(self):
        with self.assertRaisesRegex(release.ReleaseError, 'checksum'):
            self.receive(release_archive(corrupt=True))
        self.assertFalse((self.directory / 'compose.images.json').exists())

    def test_unapproved_database_source_is_rejected(self):
        with self.assertRaisesRegex(release.ReleaseError, 'fingerprint'):
            self.receive(release_archive(lambda manifest: manifest.update(database_sha256='e' * 64)))

    def test_rejects_duplicate_traversal_absolute_and_extra_paths(self):
        for name in ['podhod', '../escape', '/tmp/escape', 'Dockerfile']:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                entry = tarfile.TarInfo(name)
                entry.size = 1
                with self.assertRaises(release.ReleaseError):
                    release.receive_bundle(io.BytesIO(release_archive(extra=(entry, b'x'))), Path(directory), DATABASE)

    def test_rejects_symlinks_and_extended_headers_before_interpreting_them(self):
        for kind in [tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.XHDTYPE, tarfile.GNUTYPE_LONGNAME, tarfile.GNUTYPE_SPARSE]:
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                entry = tarfile.TarInfo('podhod')
                entry.type = kind
                entry.linkname = '/etc/passwd'
                with self.assertRaises(release.ReleaseError):
                    release.receive_bundle(io.BytesIO(release_archive(extra=(entry, b''))), Path(directory), DATABASE)

    def test_gzip_decompression_is_bounded_before_tar_metadata_parsing(self):
        metadata = tarfile.TarInfo('pax-bomb')
        metadata.type = tarfile.XHDTYPE
        metadata.size = 2 * 1024 * 1024
        raw = metadata.tobuf() + b'\0' * metadata.size
        with patch.object(release, 'MAX_EXPANDED', 1024), self.assertRaisesRegex(release.ReleaseError, 'size limit'):
            self.receive(gzip.compress(raw))

    def test_compressed_input_and_file_sizes_are_bounded(self):
        with patch.object(release, 'MAX_COMPRESSED', 32), self.assertRaisesRegex(release.ReleaseError, 'size limit'):
            self.receive(release_archive())

    def test_truncated_gzip_cannot_activate(self):
        with self.assertRaises(release.ReleaseError):
            self.receive(release_archive()[:-20])

    def test_invalid_commit_and_nonpositive_run_number_are_rejected(self):
        for field, value in [('commit', 'main'), ('run_number', 0), ('run_number', True)]:
            with self.subTest(field=field), tempfile.TemporaryDirectory() as directory:
                with self.assertRaises(release.ReleaseError):
                    release.receive_bundle(io.BytesIO(release_archive(lambda manifest: manifest.update({field: value}))), Path(directory), DATABASE)


class RecordingDeployment(release.Deployment):
    def __init__(self, root):
        super().__init__({'root': str(root)})
        self.directory.mkdir()
        self.calls = []
        self.fail_build = False
        self.fail_backup = False
        self.fail_start = False
        self.health_failures = 0
        self.interrupt_after_stop = False
        self.previous = {name: 'sha256:' + str(index) * 64 for index, name in enumerate(release.SERVICES, start=1)}
        self.replacement = {name: 'podhod-' + name + ':' + COMMIT for name in release.SERVICES}
        self.state.write_text('{"commit":"previous"}\n')

    def runtime_images(self):
        return self.previous

    def check_runtime(self, expected=None):
        return {'user_id': 1, 'google_connected': True}

    def build_images(self, directory, manifest):
        self.calls.append('build')
        if self.fail_build:
            raise release.ReleaseError('build failed')
        return self.replacement

    def compose(self, *arguments, **kwargs):
        self.calls.append(arguments)
        if arguments[:1] == ('stop',) and self.interrupt_after_stop:
            signal.raise_signal(signal.SIGTERM)
        if arguments[:1] == ('up',) and self.fail_start:
            self.fail_start = False
            raise release.ReleaseError('start failed')
        return b''

    def backup_databases(self, destination):
        self.calls.append('backup')
        if self.fail_backup:
            raise release.ReleaseError('backup failed')

    def wait_for_runtime(self, expected):
        self.calls.append('health')
        if self.health_failures:
            self.health_failures -= 1
            raise release.ReleaseError('health failed')
        return expected


class DeploymentRollbackTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.deployment = RecordingDeployment(Path(self.scratch.name))
        self.manifest = {'commit': COMMIT, 'run_number': 10, 'files': {}}

    def test_build_failure_never_stops_services(self):
        self.deployment.fail_build = True
        with self.assertRaises(release.ReleaseError):
            self.deployment.activate_release(Path(self.scratch.name), self.manifest)
        self.assertEqual(['build'], self.deployment.calls)

    def test_backup_start_and_health_failure_restore_actual_prior_image_ids(self):
        for phase in ['backup', 'start', 'health']:
            with self.subTest(phase=phase), tempfile.TemporaryDirectory() as directory:
                deployment = RecordingDeployment(Path(directory))
                previous_state = deployment.state.read_bytes()
                deployment.fail_backup = phase == 'backup'
                deployment.fail_start = phase == 'start'
                deployment.health_failures = 1 if phase == 'health' else 0
                with self.assertRaisesRegex(release.ReleaseError, 'previous application images restored'):
                    deployment.activate_release(Path(directory), self.manifest)
                override = json.loads(deployment.override.read_text())
                self.assertEqual({name: {'image': image} for name, image in deployment.previous.items()}, override['services'])
                self.assertEqual(previous_state, deployment.state.read_bytes())
                self.assertIn(('up', '-d', '--no-deps', 'api'), deployment.calls)

    def test_success_stops_writers_before_backup_and_commits_state_after_health(self):
        self.deployment.activate_release(Path(self.scratch.name), self.manifest)
        calls = self.deployment.calls
        self.assertLess(calls.index(('stop', 'api')), calls.index('backup'))
        self.assertLess(calls.index('backup'), calls.index(('up', '-d', '--no-deps', 'api')))
        self.assertEqual(10, json.loads(self.deployment.state.read_text())['run_number'])
        self.assertEqual('health', calls[-1])

    def test_termination_after_stopping_writers_restores_previous_images(self):
        self.deployment.interrupt_after_stop = True
        with self.assertRaisesRegex(release.ReleaseError, 'previous application images restored'):
            self.deployment.activate_release(Path(self.scratch.name), self.manifest)
        restored = json.loads(self.deployment.override.read_text())['services']
        self.assertEqual({name: {'image': image} for name, image in self.deployment.previous.items()}, restored)

    def test_failed_rollback_is_reported_separately(self):
        self.deployment.health_failures = 2
        with self.assertRaisesRegex(release.ReleaseError, 'ROLLBACK FAILED'):
            self.deployment.activate_release(Path(self.scratch.name), self.manifest)


class DeploymentOrderingTests(unittest.TestCase):
    def test_older_workflow_run_cannot_replace_newer_successful_release(self):
        from types import SimpleNamespace
        from unittest.mock import Mock
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            deploy = root / 'deploy'
            deploy.mkdir()
            state = {'commit': 'b' * 40, 'run_number': 11, 'files': {}}
            (deploy / 'current.json').write_text(json.dumps(state))
            config = Mock()
            config.stat.return_value = SimpleNamespace(st_uid=0, st_mode=0o600)
            config.read_text.return_value = json.dumps({'root': directory, 'approved_database_sha256': DATABASE})
            with patch.object(release, 'CONFIG', config), patch.object(release.os, 'geteuid', return_value=0), \
                    patch.dict(release.os.environ, {'SSH_ORIGINAL_COMMAND': ''}), \
                    patch.object(release.Deployment, 'activate_release') as activate:
                with self.assertRaisesRegex(release.ReleaseError, 'stale release refused'):
                    release.receive_release(io.BytesIO(release_archive()))
                activate.assert_not_called()
            self.assertEqual(state, json.loads((deploy / 'current.json').read_text()))
