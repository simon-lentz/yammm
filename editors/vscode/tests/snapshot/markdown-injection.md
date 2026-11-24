# Grammar Injection Test

Prose before the code block.

```yammm
schema "injection_test"

type Widget {
    id String primary
    label String required
}
```

Prose after the code block.

~~~yammm
type Gadget {
    name String primary
}
~~~

Every fence tag the documentation gates is highlighted as yammm.

```yammm-schema
schema "complete"
```

```yammm-snippet
type Part {
    id UUID primary
}
```

```yammm-invalid
type Broken {
```

A tag outside the vocabulary is not.

```yammm-other
type NotYammm {
```
