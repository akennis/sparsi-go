PREVIOUS ATTEMPT HAD INVALID DAG STRUCTURE.

DAG validation errors:
{{VALIDATION_ERROR}}

Previously generated code:
```go
{{GENERATED_CODE}}
```

Fix ALL DAG structural errors above before responding. Ensure:
- All op names exactly match one of the available library ops listed above.
- All Input wire names reference wires produced by an Output call of some other vertex.
- Wire names passed to Input(...) must be assigned via Output(...) in a preceding vertex.
