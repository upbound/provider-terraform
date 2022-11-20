#!/usr/bin/env bash
set -aeuo pipefail

scriptdir="$( dirname "${BASH_SOURCE[0]}")"

echo "Running setup.sh"

if [[ -n "${UPTEST_CLOUD_CREDENTIALS:-}" ]]; then
  # NOTE(turkenh): UPTEST_CLOUD_CREDENTIALS may contain more than one cloud credentials that we expect to be provided
  # in a single GitHub secret. We expect them provided as key=value pairs separated by newlines. Currently we expect
  # AWS and GCP credentials to be provided. For example:
  # AWS='[default]
  # aws_access_key_id = REDACTED
  # aws_secret_access_key = REDACTED'
  # GCP='{
  #   "type": "service_account",
  #   "project_id": "REDACTED",
  #   "private_key_id": "REDACTED",
  #   "private_key": "-----BEGIN PRIVATE KEY-----\nREDACTED\n-----END PRIVATE KEY-----\n",
  #   "client_email": "REDACTED",
  #   "client_id": "REDACTED",
  #   "auth_uri": "https://accounts.google.com/o/oauth2/auth",
  #   "token_uri": "https://oauth2.googleapis.com/token",
  #   "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
  #   "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/official-provider-testing%40official-provider-testing.iam.gserviceaccount.com"
  # }'
  eval "${UPTEST_CLOUD_CREDENTIALS}"

  if [[ -n "${AWS:-}" ]]; then
      echo "Creating cloud credentials secret for AWS..."
      ${KUBECTL} -n upbound-system create secret generic aws-creds --from-literal=credentials="${AWS}" --dry-run=client -o yaml | ${KUBECTL} apply -f -
      ${KUBECTL} apply -f "${scriptdir}/../../examples/providerconfig-aws.yaml"
  fi

  if [[ -n "${GCP:-}" ]]; then
      echo "Creating cloud credentials secret for GCP..."
      ${KUBECTL} -n upbound-system create secret generic gcp-creds --from-literal=credentials="${GCP}" --dry-run=client -o yaml | ${KUBECTL} apply -f -
      ${KUBECTL} apply -f "${scriptdir}/../../examples/providerconfig.yaml"
  fi
fi