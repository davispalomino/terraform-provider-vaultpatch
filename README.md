# Terraform Provider VaultPatch

Custom Terraform provider for partial key management in HashiCorp Vault KV v2 secrets.

Manages individual keys within a Vault secret path without affecting other keys in the same path.

## Problem

The official Vault provider (`vault_kv_secret_v2`) manages the **entire** secret. If your path has keys from other services, it will overwrite them.

This provider only touches the keys you declare — everything else stays untouched.

## Requirements

- [Go](https://golang.org/doc/install) >= 1.21
- [Terraform](https://www.terraform.io/downloads.html) >= 1.0
- HashiCorp Vault with KV v2 secrets engine

## Build & Install

```bash
make install
```

This compiles the binary and installs it to `~/.terraform.d/plugins/`.

## Usage

```hcl
terraform {
  required_providers {
    vaultpatch = {
      source  = "davispalomino/vaultpatch"
      version = "0.1.0"
    }
  }
}

provider "vaultpatch" {
  address   = "https://vault.example.com"
  role_id   = var.vault_role_id
  secret_id = var.vault_secret_id
}

resource "vaultpatch_kv_keys" "my_secrets" {
  mount = "app"
  path  = "my-service/secrets"

  keys = {
    DEMO = "my-key"
  }
}
```

## How It Works

| Operation | Behavior |
|-----------|----------|
| **Create** | Reads existing JSON, merges your keys, writes back |
| **Update** | Reads existing JSON, updates only your keys, writes back |
| **Delete** | Reads existing JSON, removes only your keys, writes back |

Other keys in the same path are **never** modified.

## Provider Configuration

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `address` | string | yes | Vault server URL |
| `role_id` | string | yes | AppRole Role ID |
| `secret_id` | string | yes | AppRole Secret ID |

## Resource: `vaultpatch_kv_keys`

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `mount` | string | yes | KV v2 mount path (e.g., `app`) |
| `path` | string | yes | Secret path within mount (e.g., `my-service/secrets`) |
| `keys` | map(string) | yes | Key-value pairs to manage |

## Import

```bash
terraform import vaultpatch_kv_keys.my_secrets app_envs/my-service/secrets
```
