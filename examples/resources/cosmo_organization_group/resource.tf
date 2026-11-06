resource "cosmo_namespace" "team" {
  name = var.namespace
}

# A group whose members can view the whole organization and administer one namespace.
resource "cosmo_organization_group" "team" {
  name        = var.name
  description = "Platform team"

  rules = [
    { role = "organization-viewer" },
    { role = "namespace-admin", namespaces = [cosmo_namespace.team.id] },
  ]

  # Needed only if people or API keys outside Terraform are still in the group when it is destroyed.
  # move_members_to_group_id = data.cosmo_organization_group.viewer.id
}
