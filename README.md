# WARNING

**Do not use in production! This is just some quick & dirty code.**

This package aims to provide a way to generate a proxy with an invocation handler for exported functions of a package.

Restrictions:

- Only exported identifiers proxied
- Only functions where all parameters use exported types can be proxied
- Only functions where all results use exported types can be proxied
