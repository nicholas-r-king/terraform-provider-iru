resource "iru_ade_integration" "example" {
  blueprint_id         = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
  email                = "enrollment@example.invalid"
  phone                = "+1 555 0100"
  server_token         = filebase64("apple-server-token.p7m")
  server_token_version = 1
}
