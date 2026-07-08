# Advanced Ticket Triage Benchmark

This benchmark evaluates the performance of Sparsi vs LangChain for a realistic multi-step workflow: **Advanced Customer Support Ticket Triage**.

## Task Description
Real-world pipelines rarely classify intent blindly. This benchmark tests a strict 5-step sequence:
1. **Information Extraction**: Extract `order_id`, `product_details`, and `user_email` from the ticket.
2. **Sentiment Analysis**: Analyze the sentiment and assign an `urgency_score` (1-5).
3. **Intent Classification**: Classify the core intent based on the raw utterance, extracted info, and sentiment.
4. **Policy Checking**: Determine the appropriate action (`escalate`, `standard_process`, `reject`) based on the intent and urgency score.
5. **Draft Response**: Draft a final polite response email to the user based on the original utterance, classified intent, and policy action.

## Dataset
We use the `bitext/Bitext-customer-support-llm-chatbot-training-dataset` from Hugging Face.

## Systems Compared
1. **Sparsi (Multi-Step DAG)**: Uses a compiled workflow graph (`dagor`) and `gemini-3.1-flash-lite` to execute the 5 steps. Each step is represented as a distinct `Operator` node. The DAG parallelizes independent steps (like Sentiment Analysis and Intent Classification) to minimize the critical path length.
2. **LangChain (ReAct Agent)**: Uses a LangChain `create_react_agent` equipped with tools matching the steps above, utilizing a goal-oriented system prompt that relies on the agent's native reasoning loop to fulfill the workflow autonomously.

## How to Run

Ensure your environment variables for `GEMINI_API_KEY` are set.

```bash
go run main.go --samples 100
```

You can adjust the number of samples with the `--samples` flag.

## Automated Prompt Optimization (Tree Search)
To ensure both frameworks were evaluated at their absolute maximum potential, we built an automated hyperparameter search script (`auto_optimize.py`) that performed an iterative, multi-generational Tree Search. The script tested radically different prompt structures (Generation 1), identified the winners, and then iteratively performed micro-mutations on those winners (Generation 2 & 3) until the models plateaued.

**Findings from the Optimization Tree Search:**
- **Sparsi Limits (14.8k tokens)**: In Generation 1, we learned that stripping the prompts down to pure fragmented shorthand (e.g., `intent: ...`) completely broke the LLM's JSON engine, dropping accuracy to 0%. However, by micro-mutating the baseline over 3 generations, we found the mathematical plateau. Sparsi can maintain 100% accuracy using extremely concise, machine-like pseudo-code (e.g. `policy_action: 'escalate' if complaint/payment_issue/contact_human...`).
- **LangChain Limits (51.8k tokens)**: When we attempted to compress the ReAct agent's prompt into dense markdown or XML tags during Generation 1, its accuracy plummeted to 70-80%. The agent *requires* significant conversational scaffolding to successfully maintain its JSON output schema and follow multi-step constraints (like appending the security token) across its internal thought loop. It plateaued immediately at its baseline.

## Final Results (100 Samples)

After running the heavily optimized prompts across 100 samples, the performance differences became starkly clear:

```text
==========================================
             BENCHMARK RESULTS            
==========================================
System                         Accuracy   Avg Latency(s)  Wall Time(s)    Total Tokens    Failures  
----------------------------------------------------------------------------------------------------
Sparsi (Multi-Step DAG)        100.00%     2.31            209.33          66586           0         
LangChain (ReAct Agent)        97.00%      3.03            225.04          264998          0         
==========================================
```

*Note: Pipeline Accuracy requires perfect intent matching, perfect policy matching, and a passing grade from an independent LLM-as-a-judge on the drafted email's formatting and politeness.*

## Conclusion

When scaling a complex, multi-step AI workflow, the architectural differences between a ReAct agent and a DAG become undeniable:

1. **Tokens (Cost)**: Sparsi is definitively cheaper, consuming less than 1/4th of LangChain's tokens (~66k vs ~265k). ReAct agents are inherently token-hungry because they must embed massive system rules, tool schemas, and their own expanding reasoning history into every single iterative loop.
2. **Latency (Speed)**: Because Sparsi can deterministically trigger independent nodes (like analyzing sentiment and classifying intent) concurrently, it achieves a noticeably faster average response latency per request. 
3. **Reliability**: Sparsi maintained a flawless **100% accuracy** at scale. Meanwhile, even with its highly conversational (and expensive) baseline prompt, the LangChain ReAct agent occasionally hallucinated or lost track of its formatting constraints during the 100-sample run, dropping to **97% accuracy**. 

For production-grade pipelines where strict adherence to formatting and low latency are critical, Sparsi's deterministic graph execution is significantly more reliable and economical than a dynamic ReAct agent.
