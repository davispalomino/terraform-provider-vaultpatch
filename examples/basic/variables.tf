variable "vault_address" {
  description = "The URL of the Vault server (e.g., https://vault.example.com)"
  type        = string
}

variable "vault_role_id" {
  description = "The AppRole Role ID for authenticating with Vault"
  type        = string
  sensitive   = true
}

variable "vault_secret_id" {
  description = "The AppRole Secret ID for authenticating with Vault"
  type        = string
  sensitive   = true
}
