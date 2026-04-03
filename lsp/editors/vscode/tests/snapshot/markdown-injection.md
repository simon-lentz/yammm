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
