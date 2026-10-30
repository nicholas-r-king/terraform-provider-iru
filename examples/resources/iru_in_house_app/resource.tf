resource "iru_in_house_app" "example" {
  name           = "Example IPA"
  file_key       = "tenants/synthetic/library/ipa_apps/example.ipa"
  active         = false
  runs_on_iphone = true
}
