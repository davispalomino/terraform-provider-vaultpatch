resource "vaultpatch_kv_keys" "service_vars" {
  mount = "app"
  path  = "my-service/vars"

  keys = {
    KAFKA_CONSUMER_GROUP_ID = "my-service-group"
  }
}
