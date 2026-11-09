# Nested Content

A 5-backtick fenced block with backtick and tilde patterns inside:

`````yammm
schema "nested"

// This content contains fence-like patterns:
// ``` not a close (only 3 backticks, need 5)
// ~~~ not a close (wrong character)

type Container {
    id String primary
    code String required
}
`````

Done.
