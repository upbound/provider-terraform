#!/usr/bin/env bash
set -e

# setting up colors
BLU='\033[0;34m'
YLW='\033[0;33m'
GRN='\033[0;32m'
RED='\033[0;31m'
NOC='\033[0m' # No Color
echo_info() {
  printf "\n${BLU}%s${NOC}" "$1"
}
echo_step() {
  printf "\n${BLU}>>>>>>> %s${NOC}\n" "$1"
}
echo_sub_step() {
  printf "\n${BLU}>>> %s${NOC}\n" "$1"
}

echo_step_completed() {
  printf "${GRN} [✔]${NOC}"
}

echo_success() {
  printf "\n${GRN}%s${NOC}\n" "$1"
}
echo_warn() {
  printf "\n${YLW}%s${NOC}" "$1"
}
echo_error() {
  printf "\n${RED}%s${NOC}" "$1"
  exit 1
}

# k8s watchers

wait_for_pods_in_namespace() {
  local timeout=$1
  shift
  namespace=$1
  shift
  arr=("$@")
  local counter=0
  for i in "${arr[@]}"; do
    echo -n "waiting for pod $i in namespace $namespace..." >&2
    while ! ("${KUBECTL}" -n $namespace get pod $i) &>/dev/null; do
      if [ "$counter" -ge "$timeout" ]; then
        echo "TIMEOUT"
        exit -1
      else ((counter += 5)); fi
      echo -n "." >&2
      sleep 5
    done
    echo "FOUND POD!" >&2
  done
}

check_deployments() {
  for name in $1; do
    echo_sub_step "inspecting deployment '${name}'"
    local dep_stat=$("${KUBECTL}" -n "$2" get deployments/"${name}")

    echo_info "check if is deployed"
    if $(echo "$dep_stat" | grep -iq 'No resources found'); then
      echo "is not deployed"
      exit -1
    else
      echo_step_completed
    fi

    echo_info "check if is ready"
    IFS='/' read -ra ready_status_parts <<<"$(echo "$dep_stat" | awk ' FNR > 1 {print $2}')"
    if (("${ready_status_parts[0]}" < "${ready_status_parts[1]}")); then
      echo "is not Ready"
      exit -1
    else
      echo_step_completed
    fi
    echo
  done
}

check_pods() {
  pods=$("${KUBECTL}" -n "${CROSSPLANE_NAMESPACE}" get pods)
  count=$(echo "$pods" | wc -l)
  if (("${count}" - 1 != "${1}")); then
    sleep 10
    "${KUBECTL}" get events -A
    sleep 20
    echo_error "unexpected number of pods"
    exit -1
  fi
  echo "$pods"
  while read -r pod_stat; do
    name=$(echo "$pod_stat" | awk '{print $1}')
    echo_sub_step "inspecting pod '${name}'"

    if $(echo "$pod_stat" | awk '{print $3}' | grep -ivq 'Completed'); then
      echo_info "is not completed, continuing with further checks"
    else
      echo_info "is completed, foregoing further checks"
      echo_step_completed
      continue
    fi

    echo_info "check if is ready"
    IFS='/' read -ra ready_status_parts <<<"$(echo "$pod_stat" | awk '{print $2}')"
    if (("${ready_status_parts[0]}" < "${ready_status_parts[1]}")); then
      echo_error "is not ready"
      exit -1
    else
      echo_step_completed
    fi

    echo_info "check if is running"
    if $(echo "$pod_stat" | awk '{print $3}' | grep -ivq 'Running'); then
      echo_error "is not running"
      exit -1
    else
      echo_step_completed
    fi

    echo_info "check if has restarts"
    if (($(echo "$pod_stat" | awk '{print $4}') > 0)); then
      echo_error "has restarts"
      exit -1
    else
      echo_step_completed
    fi
    echo
  done <<<"$(echo "$pods" | awk 'FNR>1')"
}

echo_success "Integration tests succeeded!"
