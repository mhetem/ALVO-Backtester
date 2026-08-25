-- +goose Up
ALTER TABLE chart_layouts ADD COLUMN id UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE chart_layouts ADD COLUMN name TEXT;

UPDATE chart_layouts
SET name = symbols.ticker
FROM symbols
WHERE symbols.id = chart_layouts.symbol_id;

ALTER TABLE chart_layouts DROP CONSTRAINT chart_layouts_pkey;
ALTER TABLE chart_layouts DROP COLUMN symbol_id;
ALTER TABLE chart_layouts ALTER COLUMN name SET NOT NULL;
ALTER TABLE chart_layouts ADD PRIMARY KEY (id);
ALTER TABLE chart_layouts ADD CONSTRAINT chart_layouts_user_name_key UNIQUE (user_id, name);

CREATE INDEX chart_layouts_user_idx ON chart_layouts (user_id);

-- +goose Down
DELETE FROM chart_layouts
WHERE name NOT IN (SELECT ticker FROM symbols);

ALTER TABLE chart_layouts ADD COLUMN symbol_id BIGINT REFERENCES symbols(id) ON DELETE CASCADE;

UPDATE chart_layouts
SET symbol_id = symbols.id
FROM symbols
WHERE symbols.ticker = chart_layouts.name;

DROP INDEX chart_layouts_user_idx;
ALTER TABLE chart_layouts DROP CONSTRAINT chart_layouts_user_name_key;
ALTER TABLE chart_layouts DROP CONSTRAINT chart_layouts_pkey;
ALTER TABLE chart_layouts DROP COLUMN name;
ALTER TABLE chart_layouts DROP COLUMN id;
ALTER TABLE chart_layouts ALTER COLUMN symbol_id SET NOT NULL;
ALTER TABLE chart_layouts ADD PRIMARY KEY (user_id, symbol_id);
