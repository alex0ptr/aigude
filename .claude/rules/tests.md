---
paths:
  - "**/*_test.go"
---

# Tests

## Every test answers a question no other test answers

Before adding a test, finish the sentence "This test fails when ...". If a test that exists already fails for the same bug, do not add it. Do not repeat a test under another name.

## One behavior, one layer

A behavior is tested where it lives, in the package that implements it, with fakes. `pkg/service` tests only what the layers below cannot: that the pieces are wired together, and what only exists at the HTTP boundary (status codes, headers, who sees what).

```
bad:  retry of transient speech errors in business/ (fakes) and again in pkg/service
good: the three cases in business/; one test in pkg/service that a transient error
      still yields a ready Episode
```

## A decision is tested without the service

A pure function or a single predicate (the window of the Max Lookback, the lines of the usage tooltip) gets a unit test next to it. A test that needs `servicetest.Start` and a full production for one comparison is in the wrong layer.

## A setting is tested once per mechanism

How a setting reaches its consumer is proven once. Further settings that travel the same way are not tested again; their validation is a case in the table of `pkg/settings`.

## Variants of one rule are a table

Cases that differ only in input and expected value are one table-driven test.

## Markup is tested by its content, not its shape

Assert on text and status codes, not on attribute order or class names, unless the markup is the feature (an accessible name).

## Deleting

A change that makes a test redundant removes it in the same commit. A test that only repeats a lower layer is deleted, not kept "to be safe".
