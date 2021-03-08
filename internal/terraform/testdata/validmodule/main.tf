variable "coolness" {
  default     = "very"
  description = "How cool is this test?"
}

output "coolness" {
  value       = var.coolness
  description = "The coolness of this test."
}
