-- name: FindAuthorByID :one
SELECT * FROM authors
WHERE id = ? LIMIT 1;

-- name: FindAllAuthors :many
SELECT * FROM authors
ORDER BY name;

-- name: CreateAuthor :execresult
INSERT INTO authors (
    name, bio
) VALUES (?, ?);

-- name: UpdateAuthor :execresult
UPDATE authors SET name = ?, bio = ? where id = ?;

-- name: DeleteAuthor :exec
DELETE FROM authors
WHERE id = ?;
