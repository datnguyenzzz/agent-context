#!/usr/bin/env python3
import json
import os
import urllib.request
from urllib.error import URLError, HTTPError

# ponytail: simple, zero-dependency LiteLLM validator script using Python's built-in urllib standard library

def get_env(key, default):
    return os.environ.get(key, default)

def main():
    base_url = get_env("LITELLM_BASE_URL", "http://localhost:36253/v1").rstrip("/") 
    api_key = get_env("LITELLM_API_KEY", "")
    embed_model = get_env("LITELLM_EMBEDDING_MODEL", "gemini-embedding-001")
    chat_model = get_env("LITELLM_CHAT_MODEL", "gpt-4o-mini")

    print("==================================================")
    print("      LiteLLM Connection Validator (Urllib)       ")
    print("==================================================")
    print(f"Base URL: {base_url}")
    print(f"Embedding Model: {embed_model}")
    print(f"Chat Model: {chat_model}")
    print("--------------------------------------------------")

    headers = {
        "Content-Type": "application/json"
    }
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"

    # 1. Validate Embedding API
    embed_url = f"{base_url}/embeddings"
    embed_payload = {
        "model": embed_model,
        "input": "validate connection",
        "dimensions": 1024,
    }

    print("⚙ Testing Embeddings Endpoint...")
    try:
        req = urllib.request.Request(
            embed_url,
            data=json.dumps(embed_payload).encode("utf-8"),
            headers=headers,
            method="POST"
        )
        with urllib.request.urlopen(req, timeout=5) as response:
            resp_data = json.loads(response.read().decode("utf-8"))
            vector = resp_data["data"][0]["embedding"]
            print(f"✓ Embeddings OK! Dimension: {len(vector)} (Preview: {vector[:3]}...)")
    except HTTPError as e:
        print(f"✗ Embeddings Failed! HTTP Status: {e.code}")
        print(f"  Response: {e.read().decode('utf-8')}")
        return
    except URLError as e:
        print(f"✗ Embeddings Failed! Connection error: {e.reason}")
        print("  Is LiteLLM running? Start it via: litellm --model ollama/nomic-embed-text")
        return
    except Exception as e:
        print(f"✗ Embeddings Failed! Unexpected error: {e}")
        return

    # 2. Validate Chat Completions API
    chat_url = f"{base_url}/chat/completions"
    chat_payload = {
        "model": chat_model,
        "messages": [{"role": "user", "content": "Say 'hello link'"}],
        "max_tokens": 10
    }

    print("\n⚙ Testing Chat Completions Endpoint...")
    try:
        req = urllib.request.Request(
            chat_url,
            data=json.dumps(chat_payload).encode("utf-8"),
            headers=headers,
            method="POST"
        )
        with urllib.request.urlopen(req, timeout=5) as response:
            resp_data = json.loads(response.read().decode("utf-8"))
            reply = resp_data["choices"][0]["message"]["content"]
            print(f"✓ Chat Completions OK! Response: \"{reply.strip()}\"")
    except HTTPError as e:
        print(f"✗ Chat Failed! HTTP Status: {e.code}")
        print(f"  Response: {e.read().decode('utf-8')}")
    except URLError as e:
        print(f"✗ Chat Failed! Connection error: {e.reason}")
    except Exception as e:
        print(f"✗ Chat Failed! Unexpected error: {e}")

    print("==================================================")

if __name__ == "__main__":
    main()
