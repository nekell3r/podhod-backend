import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from zipfile import ZipFile

spec = importlib.util.spec_from_file_location('import_nutrition', Path(__file__).with_name('import_nutrition.py'))
nutrition = importlib.util.module_from_spec(spec)
spec.loader.exec_module(nutrition)


class NutritionImportTests(unittest.TestCase):
    def setUp(self):
        self.selection = {'fdcID': 17, 'energyID': 2048, 'name': 'Reviewed food', 'preparation': 'Raw',
                          'category': 'protein', 'description': 'Exact source food'}
        self.dataset = {'name': 'Foundation Foods', 'release': '2026-10', 'sha256': 'a' * 64, 'url': 'https://example.org/data.zip'}
        self.raw = {'description': 'Exact source food', 'publicationDate': '2026-10-29', 'nutrients': {
            1003: {'amount': 20, 'unit': 'g'}, 1004: {'amount': 3, 'unit': 'g'},
            1005: {'amount': 0, 'unit': 'g'}, 2047: {'amount': 107, 'unit': 'kcal'},
            2048: {'amount': 112, 'unit': 'kcal'}}}

    def test_preserves_selected_energy_and_source_instead_of_recomputing_four_nine_four(self):
        food = nutrition.catalog_food(self.selection, self.raw, self.dataset)
        self.assertEqual(112, food['per100Grams']['energyKcal'])
        self.assertNotEqual(112, 20 * 4 + 3 * 9)
        self.assertEqual(2048, food['source']['nutrientIDs']['energyKcal'])
        self.assertEqual([107, 112], [item['amount'] for item in food['source']['energyValues']])
        self.assertEqual('Exact source food', food['source']['description'])
        self.assertEqual(0, food['per100Grams']['carbohydrateGrams'])

    def test_missing_nutrient_is_not_a_zero_and_missing_selected_energy_does_not_fall_back(self):
        for nutrient_id in (1003, 1004, 1005, 2048):
            with self.subTest(nutrient_id=nutrient_id):
                raw = copy.deepcopy(self.raw)
                del raw['nutrients'][nutrient_id]
                with self.assertRaisesRegex(ValueError, 'missing nutrient'):
                    nutrition.catalog_food(self.selection, raw, self.dataset)

    def test_rejects_changed_description_units_and_nonfinite_or_negative_nutrients(self):
        raw = copy.deepcopy(self.raw)
        raw['description'] = 'Similar but different food'
        with self.assertRaisesRegex(ValueError, 'description'):
            nutrition.catalog_food(self.selection, raw, self.dataset)
        for value in (-0.1, float('nan'), float('inf'), 101, True):
            with self.subTest(value=value):
                raw = copy.deepcopy(self.raw)
                raw['nutrients'][1005]['amount'] = value
                with self.assertRaises(ValueError):
                    nutrition.catalog_food(self.selection, raw, self.dataset)
        raw = copy.deepcopy(self.raw)
        raw['nutrients'][2048]['unit'] = 'kj'
        with self.assertRaisesRegex(ValueError, 'incorrect unit'):
            nutrition.catalog_food(self.selection, raw, self.dataset)

    def test_changed_archive_checksum_fails_before_parsing(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / 'source.zip'
            archive.write_bytes(b'changed')
            mapping = {'version': '1', 'foods': [{**self.selection, 'dataset': 'foundation'}],
                       'datasets': {'foundation': self.dataset}}
            with self.assertRaisesRegex(ValueError, 'checksum'):
                nutrition.build_catalog(mapping, {'foundation': archive})

    def test_foundation_duplicate_ids_are_not_silently_replaced(self):
        item = {'fdcId': 17, 'description': 'Exact source food', 'publicationDate': '2026-10-29', 'foodNutrients': []}
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / 'source.zip'
            with ZipFile(archive, 'w') as contents:
                contents.writestr('source.json', json.dumps({'FoundationFoods': [item, item]}))
            with self.assertRaisesRegex(ValueError, 'Duplicate Foundation'):
                nutrition.foundation_foods(archive, {17})

    def test_authored_recipes_only_reference_imported_foods_and_reject_zero_ready_mass(self):
        catalog = json.loads((Path(__file__).parents[1] / 'internal/service/nutrition-catalog.json').read_text())
        food_ids = {food['id'] for food in catalog['foods']}
        nutrition.validate_recipes(catalog['recipes'], food_ids)
        for field, value in [('preparedWeightGrams', 0), ('servings', float('nan'))]:
            recipe = copy.deepcopy(catalog['recipes'][0])
            recipe[field] = value
            with self.assertRaises(ValueError):
                nutrition.validate_recipes([recipe], food_ids)
        recipe = copy.deepcopy(catalog['recipes'][0])
        recipe['ingredients'][0]['foodID'] = 'unknown'
        with self.assertRaisesRegex(ValueError, 'Unknown recipe ingredient'):
            nutrition.validate_recipes([recipe], food_ids)

    def test_committed_catalog_keeps_real_olive_oil_energy_and_explicit_zero_water(self):
        catalog = json.loads((Path(__file__).parents[1] / 'internal/service/nutrition-catalog.json').read_text())
        foods = {food['id']: food for food in catalog['foods']}
        self.assertEqual(884, foods['usda-171413']['per100Grams']['energyKcal'])
        self.assertEqual(100, foods['usda-171413']['per100Grams']['fatGrams'])
        self.assertTrue(all(value == 0 for value in foods['usda-174158']['per100Grams'].values()))
        self.assertNotEqual(foods['usda-2646170']['per100Grams'], foods['usda-331960']['per100Grams'])
        self.assertEqual(24, len(foods))
        self.assertEqual(16, len(catalog['recipes']))


if __name__ == '__main__':
    unittest.main()
