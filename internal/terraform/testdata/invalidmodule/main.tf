variable "coolness" {
  default     = "very"
  description = "How cool is this test?"
}

// This output block is missing a trailing bracket.
output "coolness" {
  value       = var.coolness
  description = "The coolness of this test."
