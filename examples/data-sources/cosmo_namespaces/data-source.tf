data "cosmo_namespaces" "all" {}

locals {
  # The ID of the namespace named "tobe", or null if there is none.
  tobe_namespace_id = one([for ns in data.cosmo_namespaces.all.namespaces : ns.id if ns.name == "tobe"])

  # Or every namespace ID, keyed by name: local.namespace_ids["tobe"]
  namespace_ids = { for ns in data.cosmo_namespaces.all.namespaces : ns.name => ns.id }
}
