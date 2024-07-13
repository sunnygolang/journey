-- name: InsertTrip :one
INSERT INTO trips
    ("destination", "owner_email", "owner_name", "starts_at", "ends_at") VALUES
    ($1, $2, $3, $4, $5)
RETURNING "id";