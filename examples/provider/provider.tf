provider "vaultpatch" {
  address   = "https://vault.example.com"
  role_id   = var.vault_role_id
  secret_id = var.vault_secret_id
}
