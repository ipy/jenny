# OpenAI Responses API Feature (v1.x)

## Changes

### Responses API Support
- New provider:  for  endpoint
- Types:  with request/response structs
- Client selection via  env var

### Reasoning Effort Control
- CLI flag  threaded to providers
-  for Responses API
-  for Chat API
-  method on providers

### Thinking Block Persistence
- Transcript entries now include  and  fields
- Round-trip support for thinking blocks in multi-turn conversations
- BLK2 fixed: thinkingBlocks now persisted in engine_loop.go
- BLK3 fixed: RebuildMessages preserves Thinking/Signature from transcript

### DeepSeek Integration
- Thinking mode support via 
-  parsed and stored

