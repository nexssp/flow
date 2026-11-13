# LLM tool patterns for NFlow

Use this reference when an active AI bundle supports function tools derived from NFlow pipeline metadata. Check the bundle version, registered actions, and provider capabilities before treating the snippets as runnable; syntax alone does not register a tool implementation.

## Keep model and workflow contracts separate

- In `@schema`, omit `json` tags when Flow's default JSON key (the field name with its first letter lowercased) is the desired wire key. Add `json:"..."` only when renaming that key; keep meaningful `default` and `validate` tags.
- Schema tags are static metadata, not general expression positions. Current Flow supports declared-symbol substitution in `default` and `validate` tags when the namespace is declared first; do not assume arbitrary interpolation. See [Flow symbol rules](https://github.com/nexssp/flow/blob/main/docs/symbols.md).
- Keep the model-visible tool-argument schema separate from the pipeline's internal request/dispatch schema. Use `:tool-schema` for model arguments and `:in-schema` for the internal pipeline input; do not expose envelope fields or add dummy arguments.
- The current AI NFlow bundle requires both `:tool-schema=SchemaName` and `:tool-description="..."`. The pipeline name becomes the function name; `ai.tool_specs` derives provider-ready declarations from those modifiers and the referenced `@schema`.
- In that AI bundle, `:tool-schema=!SchemaName` is supported: the leading `!` sets `additionalProperties: false` on the tool parameter schema. This is AI-bundle-specific metadata, separate from Flow's pipeline-boundary `:in-schema` / `:out-schema` strictness. Recheck the active bundle before using it.
- `ai.tool_specs` preserves the current input and adds declarations under `ai_tool_specs`; pass that field as the model request's `tools` value instead of duplicating provider JSON.
- A zero-argument tool can use an empty schema such as `@schema ToolArguments {}`. Current Flow main supports empty object schemas; verify older or custom Flow compilers before relying on this syntax.

The AI bundle documents [function tools from NFlow metadata](https://github.com/nexssp/ai/blob/main/nflows/README.md#function-tools-from-nflow-metadata); its [`tool_specs.go`](https://github.com/nexssp/ai/blob/main/nexssflow/tool_specs.go) implementation and NFlow tests demonstrate the strict-schema marker and empty argument object. In a source file, require the AI bundle (for example, `@require github.com/nexssp/ai main as ai`) and confirm `ai.tool_specs` is available in the active host.

## Declare schemas, metadata, and workflow-local symbols

The following declarations illustrate separate model arguments and internal dispatch data. No `json` tags are needed because each default key is already correct; `default` and `validate` tags still carry behavior.

```nflow
@schema TaskText {
  task string `validate:"required,min=1"`
}

@schema ToolTaskInput {
  TaskText
  tool_call_id string `validate:"required,min=1"`
  tool_name string `validate:"required,min=1"`
  tool_arguments string `validate:"required"`
}

@schema MercuryTask {
  TaskText
  mode string `default:"tool" validate:"required,oneof=ask tool"`
}

@schema ToolArguments {}

@symbols message_roles { system, user, assistant, tool }
@symbols task_modes { ask, tool }
@symbols tool_names { count_words, text_stats }
```

Use bare symbol members for identity strings whose value is the member name. Keep these namespaces local to the workflow; they are values, not registered actions or macros.

Declare a named model once, keeping provider settings and the API-key environment-variable name in configuration rather than repeating them on every call:

```nflow
@llm assistant {
  provider: "openai-compatible",
  model: "model-name",
  api_key_env: "LLM_API_KEY"
}
```

Provider fields are bundle-specific. Keep secret values in the environment; `api_key_env` names the variable and is not the key itself.

Declare model-visible metadata on each exposed tool pipeline. Pipeline modifiers may be kept on separate indented lines; use only modifiers that the active AI bundle provides. Replace the illustrative body with a registered action or composed implementation:

```nflow
@pipeline count_words
  :tool-schema=!ToolArguments
  :tool-description="Count the words in the full user-provided task."
  :in-schema=!ToolTaskInput
  # Add the actual registered tool implementation here.
@end
```

## Derive provider tool specs and choose a tool

Build tool declarations from pipeline metadata, then pass the generated list to the named model action. `as name` binds intermediate outputs without changing the value flowing through the pipeline:

```nflow
@pipeline choose_tool
  noop as caller
  -> ai.tool_specs as prepared
  -> {
      task: caller.task,
      user: caller.task,
      system: "Choose exactly one appropriate tool. Call it with an empty argument object. Do not answer until a tool has run.",
      tools: prepared.ai_tool_specs,
      tool_choice: "required",
      max_tokens: 256
    } as request
  -> llm.assistant as selection
  -> { task: request.task, tool_calls: selection.tool_calls, model: selection.model }
@end
```

## Validate and normalize the selected call

Check the call count before indexing, bind the call once with a projection-local `let`, normalize the dispatch envelope, and route only allowlisted tool names:

```nflow
@pipeline dispatch_tool
  noop
  -> match {
      len(.tool_calls) == 1
        -> {
            let call = .tool_calls[0];
            .task,
            tool_call_id: call.id,
            tool_name: call.function.name,
            tool_arguments: call.function.arguments
          },
      _
        -> fail @{ message: "The model must return exactly one tool call" }
    }
  -> match(.tool_name) {
      tool_names.count_words
        -> pipeline.count_words,
      tool_names.text_stats
        -> pipeline.text_stats,
      _
        -> fail @{ message: "The model returned a tool name that is not allowlisted" }
    }
@end
```

A leading-dot path derives its output key from the last segment, while a bare name copies a same-named field from the current projection value. Return the complete input envelope with the result so the final response can reuse it:

```nflow
-> {
    .request.task,
    .request.tool_call_id,
    .request.tool_name,
    .request.tool_arguments,
    tool: tool_names.count_words,
    tool_result: execution.stdout
  }
```

## Complete the tool-call round trip

For the final model turn, include the original user message, the assistant's tool-call envelope, and the matching tool result. Use role symbols and the named LLM action rather than repeating identity strings or provider configuration:

```nflow
@pipeline finish_tool
  noop as tool
  -> {
      messages: [
        { role: message_roles.system, content: "Answer the original request using the tool result. Be concise and do not invent results." },
        { role: message_roles.user, content: tool.task },
        { role: message_roles.assistant, tool_calls: [{ id: tool.tool_call_id, type: "function", function: { name: tool.tool_name, arguments: tool.tool_arguments } }] },
        { role: message_roles.tool, tool_call_id: tool.tool_call_id, name: tool.tool_name, content: tool.tool_result }
      ]
    } as prompt
  -> llm.assistant as answer
  -> {
      mode: task_modes.tool,
      task,
      tool,
      tool_call_id,
      tool_result,
      answer: answer.content,
      model: answer.model
    }
@end
```

The tool pipeline bodies, model provider, API-key environment variable, and host wiring are application-specific. Treat this as a pattern, not a ready-to-run workflow; verify the current [AI bundle](https://github.com/nexssp/ai/tree/main/nexssflow) and Flow fixtures before adapting it.
