# Источники каталога продуктов IOS-06

Импорт реализован в `catalog/import_nutrition.py`: 24 проверенных продукта и 16 собственных рецептов, результат — `internal/service/nutrition-catalog.json`. Официальные выгрузки загружены в игнорируемый `.local/nutrition-data/`; в Git не включаются исходные архивы и временные распакованные данные.

```sh
python3 catalog/import_nutrition.py \
  --foundation .local/nutrition-data/foundation-2026-10.zip \
  --legacy .local/nutrition-data/sr-legacy-2018-04-csv.zip
python3 -m unittest discover -s catalog -v
```

`catalog/nutrition-food-map.json` закрепляет точный FDC ID, описание, выбранный код энергии и SHA-256 исходного архива. Импорт останавливается при отличии checksum, описания, неизвестном продукте/нутриенте, неверных единицах и недопустимых числах. Все исходные показатели энергии сохраняются рядом с выбранным; пропуски не становятся нулями. `catalog/nutrition-recipes.json` содержит собственные тексты, ингредиенты и ориентировочную готовую массу. Каталог пока не подключён к API дневника; контракт этого API согласуется отдельно.

## Зафиксированные выгрузки

Источник и условия: [USDA FoodData Central API Guide](https://fdc.nal.usda.gov/api-guide/), [страница выгрузок](https://fdc.nal.usda.gov/download-datasets/). Данные CC0; сохраняем указание USDA, точный FDC ID, исходное название, единицы и версию выгрузки.

| Выгрузка | Официальный файл | Размер | SHA-256 |
|---|---|---|---|
| Foundation Foods, октябрь 2026 | [JSON ZIP](https://fdc.nal.usda.gov/fdc-datasets/FoodData_Central_foundation_food_json_2026-10-29.zip) | 696866 байт | `dd414cc9f1a132626bad354ef6655972c78b44aff14f16114dc899708663c579` |
| SR Legacy, апрель 2018 | [CSV ZIP](https://fdc.nal.usda.gov/fdc-datasets/FoodData_Central_sr_legacy_food_csv_2018-04.zip) | 6074592 байт | `b80817294b8850530aaedf2e515c02593b1824f763a0ff356e5c2081643e6fd0` |

Foundation JSON содержит 424 записи. SR Legacy читается потоком из CSV: огромный JSON этой выгрузки не нужен. Дата релиза архива отличается от `publication_date` конкретной записи; обе сохраняются отдельно.

## Нутриенты

Коды FDC: белок 1003 (g), жир 1004 (g), углеводы по разности 1005 (g). Энергия в разных записях находится в 1008 (kcal), 2047 (Atwater General Factors, kcal) или 2048 (Atwater Specific Factors, kcal). При импорте конкретного продукта закрепляем выбранный код энергии, сохраняем альтернативные исходные значения и не заменяем их расчётом 4/9/4. Молча выбирать другой код при обновлении нельзя.

Отсутствие нутриента — неизвестное значение. Например, Foundation FDC 748608 «Oil, olive, extra virgin» не содержит полного набора КБЖУ в этих полях и не подходит для автоматического обнуления пропусков. Для оливкового масла кандидат — SR Legacy FDC 171413 «Oil, olive, salad or cooking», с отдельным точным названием: это другой описанный продукт.

У Foundation FDC 2727566–2727569 имеются отрицательные значения углеводов по разности. Такие записи не включаются в готовый калькулятор и не исправляются незаметным clamp к нулю. Выбираются другие полные проверенные записи.

## Кандидаты для ручного сопоставления

Это часть точных записей из полученных файлов. Вся актуальная выборка закреплена в `catalog/nutrition-food-map.json`; для включённых записей сверены все четыре значения, единицы и исходное описание. Перевод не изображает специфический американский продукт обычным российским аналогом.

| FDC ID | Исходное описание | Предполагаемая подпись |
|---|---|---|
| 2646170 | Chicken, breast, boneless, skinless, raw | Куриная грудка без кожи и костей, сырая |
| 331960 | Chicken, broiler or fryers, breast, skinless, boneless, meat only, cooked, braised | Куриная грудка без кожи и костей, тушёная |
| 748967 | Eggs, Grade A, Large, egg whole | Целое куриное яйцо, сырое (USDA Grade A Large) |
| 330137 | Yogurt, Greek, plain, nonfat | Греческий йогурт без добавок, обезжиренный |
| 2346396 | Oats, whole grain, rolled, old fashioned | Овсяные хлопья из цельного зерна |
| 2512381 | Rice, white, long grain, unenriched, raw | Белый длиннозёрный рис, необогащённый, сухой |
| 2512378 | Buckwheat, whole grain | Цельнозерновая гречка |
| 2346401 | Potatoes, russet, without skin, raw | Картофель russet без кожуры, сырой |
| 2346406 | Cucumber, with peel, raw | Огурец с кожурой, сырой |
| 321360 | Tomatoes, grape, raw | Томаты grape, сырые |
| 2258586 | Carrots, mature, raw | Морковь, сырая |
| 747447 | Broccoli, raw | Брокколи, сырая |
| 1105314 | Bananas, ripe and slightly ripe, raw | Банан спелый или слегка недозрелый, сырой |
| 1750341 | Apples, gala, with skin, raw | Яблоко Gala с кожурой, сырое |
| 2346394 | Nuts, walnuts, English, halves, raw | Грецкий орех, сырые половинки ядра |
| 2346393 | Nuts, almonds, whole, raw | Миндаль, сырой |
| 171413 | Oil, olive, salad or cooking | Оливковое масло для салата или приготовления (SR Legacy) |
| 173468 | Salt, table | Поваренная соль (SR Legacy) |
| 174158 | Water, bottled, generic | Питьевая бутилированная вода (SR Legacy) |

Рецепты написаны самостоятельно, с массой съедобных ингредиентов и явной готовой массой блюда. Исходные КБЖУ не подтверждают фактическую потерю нутриентов при сливании воды или жира. Температуры для курицы и яичных блюд проверены по [официальной таблице FoodSafety.gov](https://www.foodsafety.gov/food-safety-charts/safe-minimum-internal-temperatures); ссылка сохранена в соответствующих рецептах. Фотографии и тексты референса не копируются.
