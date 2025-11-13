# Connektn on AWS EKS

Production-grade deployment of Connektn on Amazon Elastic Kubernetes Service (EKS) with HA profile.

## Architecture

```
                    ┌─────────────────┐
                    │  Internet/VPN   │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │   NLB (ALB)     │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
      ┌───────▼─────┐ ┌─────▼──────┐ ┌─────▼──────┐
      │  Gateway    │ │  Gateway   │ │  Gateway   │
      │   (AZ-1)    │ │   (AZ-2)   │ │   (AZ-3)   │
      └──────┬──────┘ └──────┬─────┘ └──────┬─────┘
             │               │               │
      ┌──────▼───────────────▼───────────────▼─────┐
      │            Agent Service                     │
      └──────┬───────────────┬───────────────┬─────┘
             │               │               │
      ┌──────▼──────┐ ┌─────▼──────┐ ┌─────▼──────┐
      │   Agent     │ │   Agent    │ │   Agent    │
      │   (AZ-1)    │ │   (AZ-2)   │ │   (AZ-3)   │
      └──────┬──────┘ └──────┬─────┘ └──────┬─────┘
             │               │               │
             └───────────────┼───────────────┘
                             │
                    ┌────────▼────────┐
                    │   Amazon MSK    │
                    │     (Kafka)     │
                    └─────────────────┘
```

## Prerequisites

- AWS account with EKS cluster
- `kubectl` configured for your EKS cluster
- `helm` 3.0+
- `eksctl` (optional)
- Amazon MSK cluster or self-hosted Kafka
- AWS Secrets Manager for secret management
- (Optional) cert-manager for TLS certificates

## Setup

### 1. Create EKS Cluster

```bash
# Using eksctl
eksctl create cluster \
  --name connektn-prod \
  --region us-east-1 \
  --nodegroup-name standard-workers \
  --node-type t3.large \
  --nodes 3 \
  --nodes-min 3 \
  --nodes-max 10 \
  --managed \
  --with-oidc

# Enable IAM service accounts
eksctl utils associate-iam-oidc-provider \
  --cluster connektn-prod \
  --approve
```

### 2. Install Dependencies

#### Install cert-manager (for TLS)

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=Available --timeout=300s \
  deployment/cert-manager -n cert-manager
```

#### Install External Secrets Operator

```bash
helm repo add external-secrets https://charts.external-secrets.io
helm repo update

helm install external-secrets \
  external-secrets/external-secrets \
  -n external-secrets-system \
  --create-namespace
```

#### Install Prometheus Operator (for monitoring)

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

helm install prometheus prometheus-community/kube-prometheus-stack \
  -n monitoring \
  --create-namespace
```

### 3. Set Up Amazon MSK (Kafka)

Create an MSK cluster or use an existing one:

```bash
# Using AWS CLI
aws kafka create-cluster \
  --cluster-name connektn-kafka \
  --broker-node-group-info file://broker-info.json \
  --kafka-version "3.5.1" \
  --number-of-broker-nodes 3

# Get bootstrap servers
aws kafka get-bootstrap-brokers \
  --cluster-arn arn:aws:kafka:us-east-1:123456789012:cluster/connektn-kafka/...
```

### 4. Create Secrets in AWS Secrets Manager

```bash
# Store Stripe API key
aws secretsmanager create-secret \
  --name connektn/stripe-api-key \
  --secret-string "sk_live_your_key_here" \
  --region us-east-1

# Store webhook secret
aws secretsmanager create-secret \
  --name connektn/stripe-webhook-secret \
  --secret-string "whsec_your_secret_here" \
  --region us-east-1

# Store tenant salt
aws secretsmanager create-secret \
  --name connektn/tenant-salt \
  --secret-string "your-random-salt-min-32-chars" \
  --region us-east-1

# Store Connektn API key
aws secretsmanager create-secret \
  --name connektn/tenant-key \
  --secret-string "your_tenant_key" \
  --region us-east-1
```

### 5. Create IAM Role for External Secrets

```bash
# Create IAM policy
cat > secrets-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:DescribeSecret"
      ],
      "Resource": "arn:aws:secretsmanager:us-east-1:*:secret:connektn/*"
    }
  ]
}
EOF

aws iam create-policy \
  --policy-name ConnektnSecretsPolicy \
  --policy-document file://secrets-policy.json

# Create service account
eksctl create iamserviceaccount \
  --name connektn-secrets-sa \
  --namespace connektn \
  --cluster connektn-prod \
  --attach-policy-arn arn:aws:iam::123456789012:policy/ConnektnSecretsPolicy \
  --approve
```

### 6. Configure External Secrets

```bash
kubectl create namespace connektn

# Create SecretStore
cat <<EOF | kubectl apply -f -
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: aws-secrets-manager
  namespace: connektn
spec:
  provider:
    aws:
      service: SecretsManager
      region: us-east-1
      auth:
        jwt:
          serviceAccountRef:
            name: connektn-secrets-sa
EOF

# Create ExternalSecret
cat <<EOF | kubectl apply -f -
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: connektn-secrets
  namespace: connektn
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: SecretStore
  target:
    name: connektn-agent-secrets
  data:
    - secretKey: STRIPE_API_KEY
      remoteRef:
        key: connektn/stripe-api-key
    - secretKey: STRIPE_WEBHOOK_SECRET
      remoteRef:
        key: connektn/stripe-webhook-secret
    - secretKey: TENANT_SALT
      remoteRef:
        key: connektn/tenant-salt
    - secretKey: CONNEKTN_TENANT_KEY
      remoteRef:
        key: connektn/tenant-key
EOF
```

### 7. Create ClusterIssuer for cert-manager

```bash
cat <<EOF | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: ops@example.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - http01:
        ingress:
          class: nginx
EOF
```

### 8. Install Connektn Stack

```bash
# Update values-ha.yaml with your MSK bootstrap servers
# Then install:

helm install connektn ../../charts/connektn-stack \
  --namespace connektn \
  --values values-ha.yaml
```

### 9. Verify Installation

```bash
# Check pods
kubectl get pods -n connektn

# Check services
kubectl get svc -n connektn

# Get NLB endpoint
kubectl get svc connektn-connektn-gateway -n connektn \
  -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'

# Test health
export LB_ENDPOINT=$(kubectl get svc connektn-connektn-gateway -n connektn -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')
curl https://$LB_ENDPOINT/healthz
```

## Monitoring

### Access Prometheus

```bash
kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090
```

### Access Grafana

```bash
kubectl port-forward -n monitoring svc/prometheus-grafana 3000:80

# Default credentials: admin / prom-operator
```

## Scaling

### Manual Scaling

```bash
# Scale agents
kubectl scale deployment connektn-connektn-agent -n connektn --replicas=5

# Scale gateway
kubectl scale deployment connektn-connektn-gateway -n connektn --replicas=5
```

### Auto-scaling

HPA is already configured in `values-ha.yaml`. Monitor with:

```bash
kubectl get hpa -n connektn
```

## Upgrading

```bash
helm upgrade connektn ../../charts/connektn-stack \
  --namespace connektn \
  --values values-ha.yaml
```

## Troubleshooting

### Check MSK connectivity

```bash
kubectl run kafka-test --rm -it --image=wurstmeister/kafka -- bash
# Inside container:
kafka-broker-api-versions.sh --bootstrap-server <msk-endpoint>:9092
```

### Check external secrets

```bash
kubectl get externalsecrets -n connektn
kubectl describe externalsecret connektn-secrets -n connektn
```

### View logs

```bash
# Agent logs
kubectl logs -l app.kubernetes.io/name=connektn-agent -n connektn --tail=100

# Gateway logs
kubectl logs -l app.kubernetes.io/name=connektn-gateway -n connektn --tail=100
```

## Cleanup

```bash
helm uninstall connektn -n connektn
kubectl delete namespace connektn

# Delete MSK cluster
aws kafka delete-cluster --cluster-arn <arn>

# Delete EKS cluster
eksctl delete cluster --name connektn-prod
```

## Cost Optimization

1. **Use Spot Instances**: Configure node groups with spot instances for cost savings
2. **Right-size Resources**: Monitor actual usage and adjust resource requests/limits
3. **Scale Down**: Reduce replicas during low-traffic periods
4. **Use AWS Graviton**: Consider ARM-based instances for better price/performance

## Security Best Practices

1. **Network Policies**: Implement Kubernetes NetworkPolicies
2. **Pod Security**: Use Pod Security Standards
3. **Secrets Rotation**: Configure automatic rotation in Secrets Manager
4. **VPC Configuration**: Deploy in private subnets with NAT Gateway
5. **IAM Roles**: Use IRSA (IAM Roles for Service Accounts) for AWS access
