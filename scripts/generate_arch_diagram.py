"""Generate a high-level architecture diagram using official AWS icons.

Requires: pip install diagrams graphviz
           brew install graphviz

Usage: python3 scripts/generate_arch_diagram.py
Output: public/diagrams/architecture.png

Design principles (AWS Well-Architected diagram style):
  - Top-to-bottom flow matching the request lifecycle
  - One representative node per logical service (not per-task)
  - Labeled edges describe the interaction, not implementation details
  - Minimal edge crossings via careful cluster ordering
  - Curved splines for clean arrow routing
  - Color-coded edges: blue=DynamoDB, green=S3, pink=SQS, orange=observability
"""

import os
from diagrams import Cluster, Diagram, Edge
from diagrams.aws.compute import Fargate
from diagrams.aws.database import Dynamodb
from diagrams.aws.integration import SQS
from diagrams.aws.management import Cloudwatch
from diagrams.aws.network import ALB
from diagrams.aws.storage import S3
from diagrams.aws.general import User
from diagrams.aws.compute import ECR

OUTPUT_DIR = os.path.join(os.path.dirname(__file__), "..", "public", "diagrams")
OUTPUT_FILE = os.path.join(OUTPUT_DIR, "architecture")

with Diagram(
    "Campus Cloud Print Queue",
    filename=OUTPUT_FILE,
    outformat="png",
    show=False,
    direction="TB",
    graph_attr={
        "fontsize": "20",
        "fontname": "Helvetica Neue,Helvetica,Arial,sans-serif",
        "bgcolor": "white",
        "pad": "0.8",
        "nodesep": "1.2",
        "ranksep": "1.4",
        "dpi": "200",
        "splines": "curved",
    },
    node_attr={
        "fontsize": "12",
        "fontname": "Helvetica Neue,Helvetica,Arial,sans-serif",
    },
    edge_attr={
        "fontsize": "10",
        "fontname": "Helvetica Neue,Helvetica,Arial,sans-serif",
        "penwidth": "1.8",
    },
):

    # ── Row 1: Client ────────────────────────────────────────────────────
    client = User("Student Client")

    # ── Row 2: Ingress ───────────────────────────────────────────────────
    with Cluster(
        "AWS  \u2014  us-west-2  \u2014  VPC 10.0.0.0/16  \u2014  2 Public Subnets",
        graph_attr={
            "bgcolor": "#F7F9FC",
            "style": "rounded",
            "penwidth": "2",
            "pencolor": "#232F3E",
            "fontsize": "14",
            "fontcolor": "#232F3E",
        },
    ):

        alb = ALB("Application Load Balancer\nHTTP :80  |  Health: GET /health  |  2 AZs")

        # ── Row 3: Compute ───────────────────────────────────────────────
        with Cluster(
            "ECS Fargate Cluster",
            graph_attr={
                "bgcolor": "#FEF5E7",
                "style": "rounded",
                "fontsize": "12",
            },
        ):
            with Cluster(
                "Go Gin API Service  (2 tasks, 0.25 vCPU / 512 MiB)\n"
                "Bulkhead \u00b7 Circuit Breaker \u00b7 Rate Limiter \u00b7 Graceful Shutdown \u00b7 Request Timeouts",
                graph_attr={
                    "bgcolor": "#FCF3CF",
                    "style": "dashed",
                    "fontsize": "10",
                },
            ):
                api = Fargate("Go Gin API\n(x2 tasks)")

            with Cluster(
                "Printer Workers  (3 services x 1 task, 0.25 vCPU / 512 MiB)\n"
                "Idempotent Processing \u00b7 Graceful Shutdown (SIGTERM) \u00b7 Exponential Backoff",
                graph_attr={
                    "bgcolor": "#FADBD8",
                    "style": "dashed",
                    "fontsize": "10",
                },
            ):
                workers = Fargate("Printer Workers\n(x3 services)")

        # ── Row 4: Data (side by side) ───────────────────────────────────
        with Cluster(
            "Data & Messaging",
            graph_attr={
                "bgcolor": "#E8F8F5",
                "style": "rounded",
                "fontsize": "12",
            },
        ):
            dynamodb = Dynamodb(
                "DynamoDB\n"
                "campus-print-jobs\n"
                "PK: jobId | GSI: userId-createdAt\n"
                "On-Demand | TTL 24h"
            )

            sqs = SQS(
                "SQS Queues\n"
                "3 Standard + 3 DLQ\n"
                "Visibility 60s | Long Poll 20s\n"
                "DLQ after 3 failures"
            )

            s3 = S3(
                "S3 Bucket\n"
                "campus-print-docs\n"
                "uploads/{jobId}/{file}\n"
                "1-day lifecycle expiry"
            )

        # ── Row 5: Observability + Registry ──────────────────────────────
        with Cluster(
            "Observability & Registry",
            graph_attr={
                "bgcolor": "#FEF9E7",
                "style": "rounded",
                "fontsize": "12",
            },
        ):
            cw = Cloudwatch(
                "CloudWatch\n"
                "Structured JSON Logs (7-day)\n"
                "6-Panel Dashboard"
            )
            ecr = ECR(
                "ECR\n"
                "campus-print-api\n"
                "campus-print-worker"
            )

    # ════════════════════════════════════════════════════════════════════
    # EDGES
    # ════════════════════════════════════════════════════════════════════

    # ── Request flow (bold = primary path) ───────────────────────────────
    client >> Edge(
        label="HTTP",
        color="#232F3E",
        style="bold",
    ) >> alb

    alb >> Edge(
        label="Round Robin \u2192 TCP 8000",
        color="#FF9900",
        style="bold",
    ) >> api

    # ── API \u2192 Data (write path) ──────────────────────────────────────────
    api >> Edge(
        label="Conditional Writes\n(optimistic concurrency)",
        color="#3334CB",
    ) >> dynamodb

    api >> Edge(
        label="SendMessage\n(on job release)",
        color="#FF4F8B",
    ) >> sqs

    api >> Edge(
        label="PutObject\n(stream upload, max 50 MB)",
        color="#3F8624",
    ) >> s3

    # ── SQS \u2192 Workers ────────────────────────────────────────────────────
    sqs >> Edge(
        label="ReceiveMessage\n(1 queue per printer, Long Poll 20s)",
        color="#FF4F8B",
    ) >> workers

    # ── Workers \u2192 Data (process path, dashed = async background) ────────
    workers >> Edge(
        label="State transitions\nRELEASED\u2192PROCESSING\u2192DONE",
        color="#3334CB",
        style="dashed",
    ) >> dynamodb

    workers >> Edge(
        label="GetObject \u2192 print\nDeleteObject \u2192 cleanup",
        color="#3F8624",
        style="dashed",
    ) >> s3

    # ── Observability (dotted = telemetry) ───────────────────────────────
    api >> Edge(
        label="JSON logs\n(awslogs driver)",
        color="#FF7100",
        style="dotted",
    ) >> cw

    workers >> Edge(
        color="#FF7100",
        style="dotted",
    ) >> cw

    # ── Registry ─────────────────────────────────────────────────────────
    ecr >> Edge(
        label="Image Pull",
        color="#FF9900",
        style="dotted",
    ) >> api

    ecr >> Edge(
        color="#FF9900",
        style="dotted",
    ) >> workers


print(f"Diagram generated: {OUTPUT_FILE}.png")
