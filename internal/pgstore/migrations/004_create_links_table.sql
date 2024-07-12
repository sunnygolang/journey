CREATE TABLE IF NOT EXISTS links (
    "id"           UUID          PRIMARY KEY NOT NULL DEFAULT gen_random_uuid(),
    "trip_id"      UUID                      NOT NULL,
    "title"        VARCHAR(255)              NOT NULL,
    "url"          VARCHAR(255)              NOT NULL,

    FOREIGN KEY (trip_id) REFERENCES trips(id)
        ON UPDATE CASCADE
        ON DELETE CASCADE
);

---- create above / drop below ----

DROP TABLE IF EXISTS links;
