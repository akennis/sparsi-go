You are refining a DAG workflow design based on operator feedback. You will NOT write Go code.

# PREVIOUS DESIGN:
{{PREVIOUS_DESIGN}}

# OPERATOR FEEDBACK:
{{FEEDBACK}}

# TASK:
{{TASK}}

# AVAILABLE LIBRARY OPS:
{{LIBRARY_DESCRIPTION}}

Revise the design to address the feedback. Keep all parts of the previous design that the feedback does
not affect. Apply the same determinism rules: AI is a last resort; hardcoded data is always preferred
over AI for finite, known datasets.

# OUTPUT FORMAT
Respond ONLY with the following structured document. No Go code. No markdown outside this format.

## Workflow: [short name]

### ASCII DAG
[diagram showing vertices and data flow with → arrows]

### Vertices
List each vertex in topological order:
N. **vertex_name** — `OpName` — [Condition: pred_name] — Params: key=value, ...
   - In: FieldName ← `wire_name`
   - Out: FieldName → `wire_name`

### Predicates
- `pred_name`: which wire it reads, what value triggers it

### Custom Ops Needed
For each op not found in the library:
- **OpName**: inputs (name: type), outputs (name: type), what Run() must compute

### AI Ops Used
For each AI op in the design:
- **vertex_name** (`OpName`): the `operation` param text

### Design Rationale
Key decisions and how the feedback was addressed
