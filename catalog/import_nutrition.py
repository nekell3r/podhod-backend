#!/usr/bin/env python3
"""Build the curated nutrition catalog from pinned, local USDA archives."""

import argparse
import csv
import hashlib
import io
import json
import math
from pathlib import Path
from zipfile import ZipFile


def foundation_foods(archive, selected_ids):
    with ZipFile(archive) as source:
        names = [name for name in source.namelist() if name.endswith('.json')]
        if len(names) != 1:
            raise ValueError('Expected one Foundation Foods JSON file')
        foods = json.loads(source.read(names[0]))['FoundationFoods']
    result = {}
    for food in foods:
        if food['fdcId'] not in selected_ids:
            continue
        if food['fdcId'] in result:
            raise ValueError('Duplicate Foundation FDC ID')
        nutrients = {}
        for item in food['foodNutrients']:
            nutrient = item['nutrient']
            if 'amount' in item:
                nutrient_id = nutrient['id']
                if nutrient_id in nutrients:
                    raise ValueError(f'Duplicate nutrient {nutrient_id}')
                nutrients[nutrient_id] = {'amount': item['amount'], 'unit': nutrient['unitName'].lower()}
        result[food['fdcId']] = {
            'description': food['description'], 'publicationDate': food['publicationDate'], 'nutrients': nutrients,
        }
    return result


def legacy_foods(archive, selected_ids):
    result = {}
    with ZipFile(archive) as source:
        units = {}
        for filename in ('nutrient.csv', 'food.csv', 'food_nutrient.csv'):
            names = [name for name in source.namelist() if name.endswith('/' + filename)]
            if len(names) != 1:
                raise ValueError(f'Expected one {filename}')
            with source.open(names[0]) as contents:
                for row in csv.DictReader(io.TextIOWrapper(contents, encoding='utf-8-sig')):
                    if filename == 'nutrient.csv':
                        units[int(row['id'])] = row['unit_name'].lower()
                        continue
                    food_id = int(row['fdc_id'])
                    if food_id not in selected_ids:
                        continue
                    if filename == 'food.csv':
                        if food_id in result:
                            raise ValueError('Duplicate SR Legacy FDC ID')
                        result[food_id] = {'description': row['description'], 'publicationDate': row['publication_date'],
                                           'nutrients': {}}
                    else:
                        nutrient_id = int(row['nutrient_id'])
                        if nutrient_id in result[food_id]['nutrients']:
                            raise ValueError(f'Duplicate nutrient {nutrient_id}')
                        result[food_id]['nutrients'][nutrient_id] = {
                            'amount': float(row['amount']), 'unit': units[nutrient_id],
                        }
    return result


def catalog_food(selection, raw, dataset):
    if raw['description'] != selection['description']:
        raise ValueError(f"FDC {selection['fdcID']}: source description does not match the reviewed mapping")
    facts = {}
    nutrient_ids = {'energyKcal': selection['energyID'], 'proteinGrams': 1003,
                    'fatGrams': 1004, 'carbohydrateGrams': 1005}
    for key, nutrient_id in nutrient_ids.items():
        nutrient = raw['nutrients'].get(nutrient_id)
        unit = 'kcal' if key == 'energyKcal' else 'g'
        if nutrient is None or nutrient['unit'] != unit:
            raise ValueError(f"FDC {selection['fdcID']}: missing nutrient or incorrect unit for {key}")
        value = nutrient['amount']
        maximum = 1000 if key == 'energyKcal' else 100
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not 0 <= value <= maximum:
            raise ValueError(f"FDC {selection['fdcID']}: invalid amount for {key}")
        facts[key] = value
    if sum(value for key, value in facts.items() if key != 'energyKcal') > 101:
        raise ValueError(f"FDC {selection['fdcID']}: macros exceed 100 g with rounding tolerance")
    return {
        'id': f"usda-{selection['fdcID']}", 'name': selection['name'], 'preparation': selection['preparation'],
        'category': selection['category'], 'per100Grams': facts,
        'source': {'publisher': 'USDA FoodData Central', 'license': 'CC0-1.0',
                   'licenseURL': 'https://creativecommons.org/publicdomain/zero/1.0/',
                   'dataset': dataset['name'], 'release': dataset['release'], 'archiveSHA256': dataset['sha256'],
                   'url': dataset['url'], 'fdcID': selection['fdcID'], 'description': raw['description'],
                   'publicationDate': raw['publicationDate'], 'nutrientIDs': nutrient_ids,
                   'energyValues': [{'nutrientID': key, **raw['nutrients'][key]}
                                    for key in (1008, 2047, 2048) if key in raw['nutrients']]},
    }


def validate_recipes(recipes, food_ids):
    if not recipes or len({item['id'] for item in recipes}) != len(recipes):
        raise ValueError('Expected recipes with unique IDs')
    for recipe in recipes:
        if recipe['category'] not in {'breakfast', 'main', 'salad', 'snack'}:
            raise ValueError('Unknown recipe category')
        if not isinstance(recipe['name'], str) or not 1 <= len(recipe['name'].strip()) <= 200:
            raise ValueError('Invalid recipe name')
        for key, maximum in [('minutes', 1440), ('servings', 1000), ('preparedWeightGrams', 100000)]:
            value = recipe[key]
            if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not 0 < value <= maximum:
                raise ValueError(f'Invalid recipe {key}')
        ingredients = recipe['ingredients']
        if not 1 <= len(ingredients) <= 50 or len({item['foodID'] for item in ingredients}) != len(ingredients):
            raise ValueError('Invalid recipe ingredient count or duplicate food IDs')
        for ingredient in ingredients:
            grams = ingredient['grams']
            if ingredient['foodID'] not in food_ids:
                raise ValueError('Unknown recipe ingredient')
            if isinstance(grams, bool) or not isinstance(grams, (int, float)) or not math.isfinite(grams) or not 0 < grams <= 100000:
                raise ValueError('Invalid ingredient grams')
        if not 1 <= len(recipe['steps']) <= 30 or not all(isinstance(step, str) and 1 <= len(step.strip()) <= 2000 for step in recipe['steps']):
            raise ValueError('Invalid recipe steps')


def build_catalog(mapping, archives):
    selected = mapping['foods']
    if not mapping['version'] or not selected or len({food['fdcID'] for food in selected}) != len(selected):
        raise ValueError('Expected a versioned, nonempty mapping with unique FDC IDs')
    decoded = {}
    for key, dataset in mapping['datasets'].items():
        archive = archives[key]
        if hashlib.sha256(archive.read_bytes()).hexdigest() != dataset['sha256']:
            raise ValueError(f'{key}: archive checksum differs from reviewed source')
        ids = {food['fdcID'] for food in selected if food['dataset'] == key}
        if key == 'foundation':
            decoded[key] = foundation_foods(archive, ids)
        elif key == 'legacy':
            decoded[key] = legacy_foods(archive, ids)
        else:
            raise ValueError(f'Unsupported dataset: {key}')
    foods = []
    for selection in selected:
        key = selection['dataset']
        raw = decoded[key].get(selection['fdcID'])
        if raw is None:
            raise ValueError(f"FDC {selection['fdcID']}: selected food missing from source")
        foods.append(catalog_food(selection, raw, mapping['datasets'][key]))
    return {'formatVersion': 1, 'version': mapping['version'], 'foods': foods}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--foundation', type=Path, required=True)
    parser.add_argument('--legacy', type=Path, required=True)
    parser.add_argument('--mapping', type=Path, default=Path(__file__).with_name('nutrition-food-map.json'))
    parser.add_argument('--recipes', type=Path, default=Path(__file__).with_name('nutrition-recipes.json'))
    parser.add_argument('--output', type=Path, default=Path(__file__).parents[1] / 'internal/service/nutrition-catalog.json')
    args = parser.parse_args()
    try:
        catalog = build_catalog(json.loads(args.mapping.read_text()), {'foundation': args.foundation, 'legacy': args.legacy})
        recipes = json.loads(args.recipes.read_text())
        validate_recipes(recipes['recipes'], {food['id'] for food in catalog['foods']})
        catalog['recipes'] = recipes['recipes']
        catalog['recipeAttribution'] = {'author': recipes['author'], 'origin': recipes['origin']}
    except (KeyError, ValueError) as error:
        parser.error(str(error))
    args.output.write_text(json.dumps(catalog, ensure_ascii=False, indent=2, allow_nan=False) + '\n')
    print(f"Imported {len(catalog['foods'])} verified foods into {args.output}")


if __name__ == '__main__':
    main()
