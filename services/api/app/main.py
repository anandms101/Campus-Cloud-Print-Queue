from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app.middleware import RequestIdMiddleware, setup_logging
from app.routes import health, jobs

setup_logging()

app = FastAPI(
    title="Campus Cloud Print Queue",
    description="Distributed print job management system",
    version="1.0.0",
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.add_middleware(RequestIdMiddleware)

app.include_router(health.router)
app.include_router(jobs.router)
