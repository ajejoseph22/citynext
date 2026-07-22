-- Schema bootstrap for the CityNext appointment service.
-- This is a one-shot bootstrap, NOT a migration framework: CREATE TABLE
-- IF NOT EXISTS creates the table on first start but will not evolve an
-- existing table if the schema changes. Adequate for this fixed schema.
CREATE TABLE IF NOT EXISTS appointments (
    id          UUID        PRIMARY KEY,
    -- Mirror the domain's strings.TrimSpace: bare btrim() strips only ASCII
    -- spaces, so tab- or newline-surrounded values would slip past it. The
    -- explicit set covers ASCII whitespace (space, tab, LF, CR, FF, VT).
    -- NOTE: PostgreSQL escape strings do not support \v; an unknown escape
    -- becomes the literal character (here the letter v), which would wrongly
    -- reject names such as "Liv". Use the octal escape \013 for VT instead.
    first_name  TEXT        NOT NULL CHECK (
        char_length(first_name) BETWEEN 1 AND 100
        AND btrim(first_name, E' \t\n\r\f\013') <> ''
        AND first_name = btrim(first_name, E' \t\n\r\f\013')
    ),
    last_name   TEXT        NOT NULL CHECK (
        char_length(last_name) BETWEEN 1 AND 100
        AND btrim(last_name, E' \t\n\r\f\013') <> ''
        AND last_name = btrim(last_name, E' \t\n\r\f\013')
    ),
    visit_date  DATE        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    CONSTRAINT appointments_visit_date_unique UNIQUE (visit_date)
);
