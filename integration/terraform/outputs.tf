# Generated by: tyk-ci/wf-gen
# Generated on: Wed  9 Jun 14:47:14 UTC 2021

# Generation commands:
# ./pr.zsh -title releng: fixes and upgrade tests -branch releng/master -repos tyk,tyk-analytics,tyk-sink
# m4 -E -DxREPO=tyk


data "terraform_remote_state" "integration" {
  backend = "remote"

  config = {
    organization = "Tyk"
    workspaces = {
      name = "base-prod"
    }
  }
}

output "tyk" {
  value = data.terraform_remote_state.integration.outputs.tyk
  description = "ECR creds for tyk repo"
}

output "region" {
  value = data.terraform_remote_state.integration.outputs.region
  description = "Region in which the env is running"
}
