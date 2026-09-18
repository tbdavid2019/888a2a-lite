#!/usr/bin/env python3
"""
A2A Collaboration Envelope (a2a_envelope.py)
Structured Workflow Messaging Specification for 888a2a-lite Hub.

Provides standardized Envelope schema, serialization, validation,
and Anti-Echo Storm guards across multi-agent collaboration patterns
(Pipeline, Parallel Fan-out, Supervisor, Debate).
"""

from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
import json
import re
from typing import Any, Dict, List, Optional
import uuid


SUPPORTED_FLOW_TYPES = {"sequential", "pipeline", "parallel", "supervisor", "debate"}

CLOSING_KEYWORDS = [
    r"收錄完畢",
    r"保持連線待命",
    r"隨時準備好迎接",
    r"辛苦了",
    r"一點都不辛苦",
    r"晚安",
    r"拜拜",
    r"不用回覆",
    r"已就定位",
    r"一切正常",
    r"\[\[A2A_NO_REPLY\]\]",
]


@dataclass
class Envelope:
    """Standard multi-agent collaboration envelope.

    Travels inside the A2A task `message` payload as a structured JSON string.
    Decouples transport acknowledgement (Hub ACK) from business completion.
    """
    workflow_id: str = field(default_factory=lambda: f"wf-{uuid.uuid4().hex[:8]}")
    correlation_id: str = field(default_factory=lambda: f"corr-{uuid.uuid4().hex[:8]}")
    flow_type: str = "pipeline"  # "pipeline" | "parallel" | "supervisor" | "debate"
    step_id: str = "initial"
    reply_to: Optional[str] = None
    round: int = 1
    max_rounds: int = 3
    role: str = "worker"  # "proposer" | "critic" | "judge" | "supervisor" | "worker" | "aggregator"
    required_capabilities: List[str] = field(default_factory=list)
    terminal: bool = False  # If True, recipient MUST NOT reply (Anti-Echo Guard)
    payload: Any = field(default_factory=dict)
    metadata: Dict[str, Any] = field(default_factory=dict)
    created_at: str = field(
        default_factory=lambda: datetime.now(timezone.utc).isoformat()
    )

    def validate(self) -> None:
        """Validate envelope fields."""
        if not isinstance(self.workflow_id, str) or not self.workflow_id.strip():
            raise ValueError("workflow_id cannot be empty")
        if not isinstance(self.correlation_id, str) or not self.correlation_id.strip():
            raise ValueError("correlation_id cannot be empty")
        if not isinstance(self.flow_type, str) or self.flow_type not in SUPPORTED_FLOW_TYPES:
            raise ValueError(
                f"Unsupported flow_type '{self.flow_type}'. Must be one of {SUPPORTED_FLOW_TYPES}"
            )
        if not isinstance(self.step_id, str) or not self.step_id.strip():
            raise ValueError("step_id cannot be empty")
        if self.reply_to is not None and not isinstance(self.reply_to, str):
            raise ValueError("reply_to must be a string or null")
        if not isinstance(self.role, str) or not self.role.strip():
            raise ValueError("role cannot be empty")
        if not isinstance(self.required_capabilities, list) or any(
            not isinstance(capability, str) for capability in self.required_capabilities
        ):
            raise ValueError("required_capabilities must be a list of strings")
        if not isinstance(self.terminal, bool):
            raise ValueError("terminal must be a boolean")
        if not isinstance(self.metadata, dict):
            raise ValueError("metadata must be an object")
        if not isinstance(self.created_at, str) or not self.created_at.strip():
            raise ValueError("created_at must be a non-empty string")
        if isinstance(self.round, bool) or not isinstance(self.round, int) or self.round < 1:
            raise ValueError("round must be >= 1")
        if isinstance(self.max_rounds, bool) or not isinstance(self.max_rounds, int) or self.max_rounds < 1:
            raise ValueError("max_rounds must be >= 1")
        if self.round > self.max_rounds and not self.terminal:
            raise ValueError(
                f"round ({self.round}) exceeds max_rounds ({self.max_rounds}), envelope must be terminal"
            )

    def to_json(self) -> str:
        """Serialize envelope to JSON string suitable for A2A message payload."""
        self.validate()
        return json.dumps(asdict(self), ensure_ascii=False)

    @classmethod
    def from_json(cls, raw: str) -> "Envelope":
        """Parse envelope from JSON string with strict validation.

        If raw string is structured JSON intending to be an envelope, validates
        fields strictly and raises ValueError on malformed envelopes.
        Only pure legacy non-JSON text or non-envelope payloads are wrapped safely.
        """
        if isinstance(raw, str):
            trimmed = raw.strip()
            if trimmed.startswith("{"):
                try:
                    data = json.loads(trimmed)
                except json.JSONDecodeError as err:
                    raise ValueError(f"Malformed JSON in envelope string: {err}")
            else:
                # Legacy plain-text message without JSON formatting
                return cls(payload={"text": raw}, flow_type="pipeline", role="worker")
        elif isinstance(raw, dict):
            data = raw
        else:
            return cls(payload={"text": str(raw)}, flow_type="pipeline", role="worker")

        # Detect if dictionary is intended to be an Envelope
        envelope_markers = {"workflow_id", "flow_type", "correlation_id", "step_id"}
        if isinstance(data, dict) and any(k in data for k in envelope_markers):
            missing = sorted(envelope_markers - data.keys())
            if missing:
                raise ValueError(f"Envelope is missing required fields: {', '.join(missing)}")
            try:
                env = cls(
                    workflow_id=data["workflow_id"],
                    correlation_id=data["correlation_id"],
                    flow_type=data["flow_type"],
                    step_id=data["step_id"],
                    reply_to=data.get("reply_to"),
                    round=data.get("round", 1),
                    max_rounds=data.get("max_rounds", 3),
                    role=data.get("role", "worker"),
                    required_capabilities=data.get("required_capabilities", []),
                    terminal=data.get("terminal", False),
                    payload=data.get("payload"),
                    metadata=data.get("metadata", {}),
                    created_at=data.get("created_at", datetime.now(timezone.utc).isoformat()),
                )
            except (TypeError, ValueError) as err:
                raise ValueError(f"Invalid field types in envelope JSON: {err}")

            # Strict validation: will raise ValueError if invalid
            env.validate()
            return env

        # Fallback: dictionary is arbitrary non-envelope business data
        return cls(payload=data, flow_type="pipeline", role="worker")

    def is_anti_echo_triggered(self) -> bool:
        """Check if message content or terminal state triggers Anti-Echo Guard."""
        if self.terminal:
            return True
        # Strictly exceeded maximum allowable rounds
        if self.round > self.max_rounds:
            return True

        # Check payload text for closing markers
        text_content = ""
        if isinstance(self.payload, str):
            text_content = self.payload
        elif isinstance(self.payload, dict):
            text_content = str(self.payload.get("text", "")) + " " + str(self.payload.get("content", ""))

        for pat in CLOSING_KEYWORDS:
            if re.search(pat, text_content):
                # Only suppress if there are no explicit question marks asking for response
                if not any(q in text_content for q in ("?", "？", "請回答", "請評估")):
                    return True

        return False

    def next_step(
        self,
        next_step_id: str,
        role: str,
        payload: Any,
        terminal: bool = False,
        increment_round: bool = False,
    ) -> "Envelope":
        """Derive the next step envelope in the workflow chain."""
        next_round = self.round + 1 if increment_round else self.round
        # The maximum round is still an active round. A handoff or final
        # response may legitimately be sent at exactly max_rounds; only a
        # step beyond the limit is forced terminal.
        is_term = terminal or (next_round > self.max_rounds)
        return Envelope(
            workflow_id=self.workflow_id,
            correlation_id=self.correlation_id,
            flow_type=self.flow_type,
            step_id=next_step_id,
            reply_to=self.step_id,
            round=next_round,
            max_rounds=self.max_rounds,
            role=role,
            required_capabilities=self.required_capabilities,
            terminal=is_term,
            payload=payload,
            metadata={**self.metadata, "parent_step": self.step_id},
        )
