terraform {
  required_providers {
    vaultpatch = {
      source  = "davispalomino/vaultpatch"
      version = "1.1.0"
    }
  }
}

provider "vaultpatch" {
  address   = var.vault_address
  role_id   = var.vault_role_id
  secret_id = var.vault_secret_id
}

resource "vaultpatch_kv_keys" "vault_vars" {
  mount = "app"
  path  = "demo/service-demo/vars"

  keys = {
    SERVICE_DEMO = "demo-value"
  }
}
