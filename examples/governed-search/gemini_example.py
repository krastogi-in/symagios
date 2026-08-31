"""Google Gemini — custom function declaration only (no google_search built-in)."""

import google.generativeai as genai

from symagios_search import symagios_egress_search

web_search_fn = genai.protos.FunctionDeclaration(
    name="web_search",
    description="Search the web via Symagios's governed search gateway.",
    parameters={
        "type": "object",
        "properties": {"query": {"type": "string"}},
        "required": ["query"],
    },
)

model = genai.GenerativeModel(
    "gemini-2.5-pro",
    tools=[genai.protos.Tool(function_declarations=[web_search_fn])],
)

chat = model.start_chat()
response = chat.send_message("What's the latest on governed AI agent security?")

for part in response.candidates[0].content.parts:
    if part.function_call and part.function_call.name == "web_search":
        query = part.function_call.args["query"]
        result = symagios_egress_search(query)
        follow_up = chat.send_message(
            genai.protos.Content(
                parts=[
                    genai.protos.Part(
                        function_response=genai.protos.FunctionResponse(
                            name="web_search",
                            response={"content": result},
                        )
                    )
                ]
            )
        )
        print(follow_up)
        break
else:
    print(response)
