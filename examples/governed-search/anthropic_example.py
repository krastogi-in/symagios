"""Anthropic Messages API — custom web_search tool only (no built-in search)."""

import anthropic

from symagios_search import symagios_egress_search

client = anthropic.Anthropic()

tools = [
    {
        "name": "web_search",
        "description": "Search the web for current information via Symagios's governed search gateway.",
        "input_schema": {
            "type": "object",
            "properties": {
                "query": {"type": "string", "description": "The search query"},
            },
            "required": ["query"],
        },
    }
]

response = client.messages.create(
    model="claude-sonnet-4-20250514",
    max_tokens=1024,
    tools=tools,
    messages=[{"role": "user", "content": "What's the latest on governed AI agent security?"}],
)

messages = [{"role": "user", "content": "What's the latest on governed AI agent security?"}]
messages.append({"role": "assistant", "content": response.content})

for block in response.content:
    if block.type == "tool_use" and block.name == "web_search":
        result = symagios_egress_search(block.input["query"])
        messages.append(
            {
                "role": "user",
                "content": [
                    {
                        "type": "tool_result",
                        "tool_use_id": block.id,
                        "content": result,
                    }
                ],
            }
        )

follow_up = client.messages.create(
    model="claude-sonnet-4-20250514",
    max_tokens=1024,
    tools=tools,
    messages=messages,
)
print(follow_up)
