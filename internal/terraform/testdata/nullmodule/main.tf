variable "coolness" {
  default     = "very"
  description = "How cool is this test?"
}

output "coolness" {
  value       = var.coolness
  description = "The coolness of this test."
}

output "randomness" {
  value       = random_id.test.hex
  description = "A random string."
}

terraform {
  required_providers {
    null = {
      version = "3.2.4"
    }
    random = {
      version = "3.7.1"
    }
  }
}

resource "random_id" "test" {
  byte_length = 4
}

resource "null_resource" "test" {
  triggers = {
    trigger = random_id.test.hex
  }
}
