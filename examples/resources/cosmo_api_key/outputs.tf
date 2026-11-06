output "id" {
  value = cosmo_api_key.ci.id
}

output "key" {
  value     = cosmo_api_key.ci.key
  sensitive = true
}
