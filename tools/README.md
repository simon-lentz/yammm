# ANTLR tooling

This directory is used to cache the ANTLR tool JAR used when regenerating the Yammm grammar. The `Makefile`
target `generate-grammars` will download `antlr-$(ANTLR_VERSION)-complete.jar` here automatically if it is
missing so contributors don't have to manage the binary manually.
