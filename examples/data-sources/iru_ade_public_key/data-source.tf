data "iru_ade_public_key" "example" {}

output "ade_public_key_pem" {
  value = data.iru_ade_public_key.example.public_key_pem
}

output "ade_public_key_response_sha256" {
  value = data.iru_ade_public_key.example.sha256
}
