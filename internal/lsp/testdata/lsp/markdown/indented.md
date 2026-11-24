# Indented Fences

1-space indented opening (skipped):

 ```yammm
schema "one_space"
 ```

3-space indented opening (skipped):

   ```yammm
schema "three_space"
   ```

Zero-indent opening with 3-space indented closing (valid):

```yammm
schema "valid_indent"

type IndentClose {
    id String primary
}
   ```
