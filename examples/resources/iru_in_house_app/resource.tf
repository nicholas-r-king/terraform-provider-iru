resource "iru_in_house_app" "example" {
  name           = "Example IPA"
  file_key       = "tenants/synthetic/library/ipa_apps/example.ipa"
  active         = false
  runs_on_iphone = true
}

resource "iru_in_house_app" "local_installer" {
  name           = "Local IPA example"
  source_file    = "./installers/example.ipa"
  source_sha256  = filesha256("./installers/example.ipa")
  active         = false
  runs_on_iphone = true
}
