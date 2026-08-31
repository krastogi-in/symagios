"""OpenAI Responses API — custom function tool only (no built-in web_search)."""

import json

from openai import OpenAI

from symagios_search import symagios_egress_search

client = OpenAI()

tools = [
    {
        "type": "function",
        "name": "web_search",
        "description": "Search the web via Symagios's governed search gateway.",
        "parameters": {
            "type": "object",
            "properties": {"query": {"type": "string"}},
            "required": ["query"],
        },
    }
]

response = client.responses.create(
    model="gpt-4.1",
    tools=tools,
    input="What's the latest on governed AI agent security?",
)

input_items = [{"role": "user", "content": "What's the latest on governed AI agent security?"}]
input_items.extend(response.output)

for item in response.output:
    if item.type == "function_call" and item.name == "web_search":
        args = json.loads(item.arguments)
        result = symagios_egress_search(args["query"])
        input_items.append(
            {
                "type": "function_call_output",
                "call_id": item.call_id,
                "output": result,
            }
        )

follow_up = client.responses.create(
    model="gpt-4.1",
    tools=tools,
    input=input_items,
)
print(follow_up)
