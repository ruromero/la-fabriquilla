# Ollama Model Configurations

Tuned Modelfiles for the RTX 3060 12GB + 48GB DDR4 setup.

## Creating models

From this directory on the Ollama server:

```bash
for f in models/*.Modelfile; do
  name=$(basename "$f" .Modelfile)
  ollama create "$name" -f "$f"
done
```

## Models

| Modelfile | Base | num_ctx | Notes |
|-----------|------|---------|-------|
| qwen2.5-coder-7b-tuned | qwen2.5-coder:7b | 16384 | Fully on-GPU, fast |
| qwen2.5-coder-14b-tuned | qwen2.5-coder:14b | 8192 | Tight VRAM fit |
| qwen14b-opencode-tuned | qwen14b-opencode:latest | 8192 | Tight VRAM fit |
| gemma4-12b-tuned | gemma4:12b | 16384 | Good headroom |
| qwen3.5-35b-a3b-tuned | qwen3.5:35b-a3b | 32768 | MoE, experts offload to RAM |
