---
description: Enforce PHPUnit placement, attributes, and data providers in invoicing/
paths:
  - 'invoicing/tests/**/*.php'
---

# PHP testing tooling

## Layer

- Load the `canon:test-craft` skill to pick the layer a test belongs at before choosing the tooling below, and report it rather than proceeding silently when the skill does not resolve.

## Framework

- Use PHPUnit. Place tests under `tests/`, mirroring the `src/` namespace layout.
- Name each file `<Class>Test.php` and mark each test class `final`.
- Name test methods `test<Behavior>` so the name reads as the assertion.

## Attributes

- Use PHP attributes (`#[DataProvider]`, `#[CoversClass]`). Do not use docblock annotations.
- Declare data providers `public static` and yield named cases.

## Assertions

- Use `self::assertSame` over `assertEquals` unless loose comparison is the point.
- Use `$this->expectException(...)` before the call for an expected exception. Do not wrap the call in `try`.

## Isolation

- Run database tests against a real MariaDB. Do not mock PDO.
- Do not make real network calls. Pass a fake of the dependency instead.
