output "string" {
  value = "very"
}

output "sensitive" {
  value     = "very"
  sensitive = true
}

output "tuple" {
  value = ["a", "really", "long", "tuple"]
}

output "object" {
  value = {
    wow : "suchobject"
  }
}

output "bool" {
  value = true
}

output "number" {
  value = 42
}
