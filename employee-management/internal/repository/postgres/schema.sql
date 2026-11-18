CREATE TABLE IF NOT EXISTS departments (
    id         BIGSERIAL    PRIMARY KEY,
    name       VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Department names are unique ignoring case. The database enforces it, so
-- two concurrent inserts of the same name cannot both succeed.
CREATE UNIQUE INDEX IF NOT EXISTS departments_name_lower_key ON departments (lower(name));

CREATE TABLE IF NOT EXISTS employees (
    id            BIGSERIAL     PRIMARY KEY,
    name          VARCHAR(100)  NOT NULL,
    age           INT           NOT NULL,
    position      VARCHAR(100)  NOT NULL,
    department_id BIGINT        NOT NULL REFERENCES departments (id),
    salary        NUMERIC(14,2) NOT NULL CHECK (salary > 0),
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

-- Speeds up GET /departments/{id}/employees; only active rows are indexed.
CREATE INDEX IF NOT EXISTS employees_department_id_idx
    ON employees (department_id) WHERE deleted_at IS NULL;
