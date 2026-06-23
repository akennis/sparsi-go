# Ticket Triage Benchmark

This benchmark evaluates the performance of Sparsi vs a Sequential Baseline for a realistic multi-step workflow: Context-Aware Customer Support Ticket Triage.

## Task Description
Real-world pipelines rarely classify intent blindly. This benchmark tests a strict 2-step sequence:
1. **Context Extraction**: The system must extract the tone, language, and key entities from the ticket.
2. **Intent Classification**: The system uses the raw utterance *combined* with the extracted context to classify the message into one of 27 predefined intents.

## Dataset
We use the `bitext/Bitext-customer-support-llm-chatbot-training-dataset` from Hugging Face.

## Systems Compared
1. **Sparsi (Multi-Step DAG)**: Uses a compiled workflow graph (`dagor`) and `gemini-3.1-flash-lite` to execute the two steps. Node 1 extracts context and feeds it directly into Node 2 which classifies the intent. This deterministic routing guarantees execution order.
2. **LangChain (ReAct Equivalent)**: A ReAct tool-calling loop using the Google GenAI Go SDK. It equips an agent loop with two explicit tools and executes them dynamically, fully simulating the dynamic overhead of a typical LangChain ReAct agent.

## How to Run

Ensure your environment variables for `GEMINI_API_KEY` or `GOOGLE_API_KEY` are set.

```bash
go run main.go --samples 500
```

You can adjust the number of samples with the `--samples` flag.

## Metrics
The benchmark measures:
- **Accuracy**: Percentage of correctly identified intents.
- **Avg Latency (s)**: Average time taken per request.
- **Wall Time (s)**: Total processing time for the batch.
- **Failures**: Number of failed extractions.
- **Total Tokens**: The sum of input and output tokens consumed across all LLM calls.

### Results
Based on a run of 500 samples from `bitext/Bitext-customer-support-llm-chatbot-training-dataset` using `gemini-3.1-flash-lite`:

| System | Accuracy | Avg Latency (s) | Wall Time (s) | Total Tokens | Failures |
| :--- | :--- | :--- | :--- | :--- | :--- |
| Sparsi (Multi-Step DAG) | 98.20% | 1.09 | 9.74 | 184,199 | 0 |
| LangChain (ReAct Equivalent) | 99.00% | 6.13 | 54.44 | 1,950,552 | 0 |

### Conclusion
When tasked with a realistic multi-step pipeline, Sparsi's deterministic DAG architecture provides an enormous advantage. While both systems achieved comparable high accuracy and operated at zero failure rates, the ReAct loop's dynamic tool routing caused its token consumption to skyrocket to nearly **2 million tokens (a ~10.5x increase)** and its total wall time to jump to **54 seconds (a ~5.5x slowdown)**. Additionally, Sparsi achieved a remarkably low average execution latency of ~1.09s compared to LangChain's ~6.13s. Sparsi executed the exact same logic sequence with dramatically less token overhead and significantly less processing time.
