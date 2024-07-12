CREATE TABLE IF NOT EXISTS activities (
    "id"           UUID          PRIMARY KEY NOT NULL DEFAULT gen_random_uuid(),
    "trip_id"      UUID                      NOT NULL,
    "title"        VARCHAR(255)              NOT NULL,
    "occurs_at"    TIMESTAMP                 NOT NULL,

    FOREIGN KEY (trip_id) REFERENCES trips(id)
        ON UPDATE CASCADE
        ON DELETE CASCADE
);

---- create above / drop below ----

DROP TABLE IF EXISTS activities;
