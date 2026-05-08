English | [中文](README.zh.md)

# Dagor

Dagor is a high-performance DAG (Directed Acyclic Graph) operator execution framework designed for high-concurrency online services. It decouples complex business logic into independent operators, enabling flexible orchestration via DAGs with automated parallel scheduling and data injection.

It is ideal for industrial-grade scenarios such as search engines, recommendation systems, advertising platforms, and real-time feature engineering.

## ✨ Key Highlights

* **Field-Level Dependency**: The framework automatically deduces vertex dependencies; users only need to declare input/output fields.
* **Zero-Code Injection**: Automated mapping of input/output fields and seamless data transmission between operators.
* **Configuration-Driven**: Define complex business workflows via JSON, achieving complete decoupling of business topology from code logic.
* **Extreme Performance**: Features a goroutine pool for asynchronous scheduling, operator pooling, and topology optimization to maximize parallelism and minimize GC pressure.
* **Developer-Friendly API**: Clean JSON syntax and out-of-the-box APIs allow developers to focus purely on core business logic.
* **Code Generation**: Automated generation of operator code to reduce manual development effort.

## 🧩 Core Concepts

* **Operator**: The independent unit of computation containing specific business logic.
* **Vertex**: A node in the graph. Each vertex corresponds to a specific Operator instance.
* **Edge**: Represents a dependency between vertices, corresponding to an output data field (variable) from one vertex.
* **Graph**: A DAG composed of multiple vertices and edges, representing a complete business workflow.
* **Engine**: The runtime container for the Graph. It handles goroutine scheduling, state management, and variable injection.

Relationship between **Graph**、**Vertex** and **Operator**:
![dag](/docs/images/dag.png)

## 📦 Installation

```bash
go get github.com/wwz16/dagor
```

## 🚀 Quick Start

Below is a minimalist mathematical calculation example. For the full example, see [examples/math/](/examples/math/).

### 1. Define an Operator

Take `AddOp` as an example. Use the `dag` tag to declare inputs and outputs; the framework will automatically handle data binding.

```go
import (
    "context"
    "fmt"
    "log"

    "github.com/wwz16/dagor/config"
    "github.com/wwz16/dagor/operator"
)

type AddOp struct {
    a   *int `dag:"input"`
    b   *int `dag:"input"`
    sum int  `dag:"output"`
}

// Setup parses and validates params and setup internal fields.
func (op *AddOp) Setup(params *config.Params) error {
    return nil
}

// Run executes the operator.
func (op *AddOp) Run(ctx context.Context) error {
    if op.a == nil || op.b == nil {
        return fmt.Errorf("AddOp: missing required input 'a' or 'b'")
    }
    op.sum = *op.a + *op.b
    return nil
}

// Reset resets the operator state and clear internal fields in order to reuse next time.
func (op *AddOp) Reset() error {
    return nil
}

func init() {
    // register operator
    if err := operator.RegisterOp[AddOp](); err != nil {
        log.Fatalf("RegisterOp[AddOp] error: %v", err)
    }
}
```

**Conventions:**

* Use `dag:"input"` for input fields and `dag:"output"` for output fields.
* Input fields must be **pointer types** (`*int`, `*string`, etc.) for high-efficiency transmission.
* Input fields are **read-only** to ensure concurrency safety.

### 2. Configure the Graph

Prepare a JSON configuration to define the topology.

```json
{
  "name": "math_demo", // graph name
  "vertices": { // all vertices
    "const10": { // vertex name
      "op": "ConstOp", // operator class name
      "params": { // operator parameters
        "in": 10
      },
      "outputs": {  // output data
        "out": "n1"  // `out` is operator field name that defined in operator class, `n1` is vertex field name that used for graph dependencies
      }
    },
    "const20": {
      "op": "ConstOp",
      "params": {
        "in": 20
      },
      "outputs": {
        "out": "n2"
      }
    },
    "add": {
      "op": "AddOp",
      "inputs": {
        "a": "n1",
        "b": "n2"
      },
      "outputs": {
        "result": "n3"
      }
    },
    "log": {
      "op": "LogOp",
      "params": {
        "base": 10
      },
      "inputs": {
        "x": "n3"
      },
      "outputs": {
        "result": "answer"
      }
    }
  }
}
```

Visualize the dag:

![math demo](/docs/images/demo.png)

**Conventions:**

* vertex name must be **globally unique**
* vertex output field name must be **globally unique**

### 3. Run the Engine

```go
import (
    "log"
    "fmt"

    "github.com/wwz16/dagor"
    "github.com/panjf2000/ants/v2"
)

func main() {
    // 1. Init global goroutine pool. 
    // Take ants as an example, you may change to other pools.
    p, err := ants.NewPool(3)
    if err != nil {
        log.Printf("ants.NewPool error %v\n", err)
        return
    }
    defer p.Release()

    // 2. Build graph.
    conf := `{
      "name": "math_demo",
      ...
    }`
    g, err := dagor.NewGraphFromJson(conf)
    if err != nil {
        log.Printf("NewGraphFromJson error %v\n", err)
        return
    }

    // 3. Run graph engine
    // Init context.
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    // Create engine instance
    eng, err := dagor.NewEngine(g, p)
    if err != nil {
        log.Printf("NewEngine error %v\n", err)
        return
    }
    defer eng.Close(ctx)

    // Run the graph.
    if err = eng.Run(ctx); err != nil {
        log.Printf("Run error %v\n", err)
        return
    }

    // 4. Get the output data.
    v, ok := eng.GetOutput("answer")
    if !ok {
        log.Printf("GetOutput error\n")
        return
    }
    res := *v.(*float64)
    log.Printf("result: %f\n", res)
}
```

## 🛠 Advanced Features

### Automated Code Generation

Implementing every method of the `IOperator` interface can be repetitive. `daggen` automates this process.

1.**Add directives to your operator file:**

```go
//go:generate daggen -type=AddOp -output=add_op_gen.go
//go:generate daggen -type=ConstOp -output=const_op_gen.go
//go:generate daggen -type=LogOp -output=log_op_gen.go
```

2.**Run generation:**

```bash
go generate ./...
```

### Dynamic Parameter Parsings

Operators can read parameters directly using `Params`, which supports path-based access without pre-defining structures.

```go
func (op *MyOp) Setup(params *config.Params) error {
    // Supports nested path access like "a.b.c" or "array.0"
    op.threshold = params.GetFloat64("config.nodes.0.threshold", 0.5)
    return nil
}
```

### Visualization

Use the `dagviz` tool to convert complex JSON configurations into intuitive topological diagrams for easier review and debugging.

```bash
python dagviz.py -i demo.json -o workflow.png
```

## 📄 License

Distributed under the [MIT License](/LICENSE).
