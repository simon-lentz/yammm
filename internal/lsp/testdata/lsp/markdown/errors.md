# Error Test

This file contains one block with syntax errors and one valid block.

## Invalid Block

```yammm
schema "errors_invalid"

type Broken {
    not valid syntax!!!
}
```

## Valid Block

```yammm
schema "errors_valid"

type Working {
    id String primary
    name String required
}
```
