"""Command line interface."""

from __future__ import annotations

from pathlib import Path
from typing import Annotated

import typer

from reservewatch import DISCLAIMER, __version__
from reservewatch.synthetic import Fmt, generate_synthetic

app = typer.Typer(no_args_is_help=True, help=f"reservewatch {__version__}. {DISCLAIMER}")


@app.command("generate-synthetic")
def generate_synthetic_cmd(
    out: Annotated[Path, typer.Option(help="Output directory.")] = Path("data/synthetic"),
    seed: Annotated[int, typer.Option(help="Seed; same seed gives identical files.")] = 42,
    fmt: Annotated[Fmt, typer.Option("--format", help="File format.")] = Fmt.CSV,
) -> None:
    """Write synthetic sample data (a clean and a deliberately violating scenario)."""
    for path in generate_synthetic(out, seed=seed, fmt=fmt):
        typer.echo(str(path))
    typer.echo(f"Synthetic data only. {DISCLAIMER}")


@app.command("check")
def check_cmd() -> None:
    """Run checks and write a report. Not implemented yet (Phase 4 wiring)."""
    typer.echo("check is not implemented yet.", err=True)
    raise typer.Exit(code=2)
