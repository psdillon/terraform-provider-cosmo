resource "cosmo_organization_group" "ci" {
  name        = var.group_name
  description = "Publishes subgraphs from CI"

  rules = [
    { role = "subgraph-publisher" },
  ]
}

resource "cosmo_api_key" "ci" {
  name     = var.name
  group_id = cosmo_organization_group.ci.id
  expires  = "1_year"
}

# Keys can also join an existing group, such as the built-in admin group.
# data "cosmo_organization_group" "admin" {
#   name = "admin"
# }
