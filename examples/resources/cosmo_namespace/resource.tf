resource "cosmo_namespace" "test" {
  name = var.name
}

# A namespace with schema proposals turned on (needs a Cosmo plan that includes
# proposals). Subgraph checks that match no approved or draft proposal only warn,
# while publishes that match no approved proposal are rejected.
resource "cosmo_namespace" "with_proposals" {
  name = "${var.name}-proposals"

  proposals = {
    check_severity_level   = "warn"
    publish_severity_level = "error"
  }
}
