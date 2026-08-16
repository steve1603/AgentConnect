"""Prompt construction for the three roundtable phases.

Every prompt here asks only for normal visible output. Nothing in this module
requests a provider's hidden chain-of-thought.
"""

from __future__ import annotations

from typing import Iterable, Mapping, Sequence

TRUNCATION_NOTE = "\n\n[...truncated for length...]"

INDEPENDENT_SYSTEM = (
    "You are one member of a three-model roundtable alongside two other AI "
    "assistants. Answer the user's request directly, completely, and in your "
    "own voice. You cannot see the other members' answers yet, so do not "
    "speculate about them. State your reasoning and your assumptions in the "
    "answer itself, flag genuine uncertainty rather than hiding it, and do "
    "not pad the response."
)

REVIEW_SYSTEM = (
    "You are reviewing draft answers written independently by the members of "
    "an AI roundtable, one of which is your own. Be a rigorous, specific "
    "critic: name factual errors, missing requirements, unstated assumptions, "
    "and points where the drafts genuinely disagree, and say which position is "
    "better supported and why. Credit stronger ideas explicitly, including "
    "ones that are not yours. Do not rewrite the full answer and do not "
    "summarise the drafts back; produce only the critique."
)

CHAIR_SYSTEM = (
    "You are the chair of an AI roundtable. You receive the user's request, "
    "each member's independent answer, and each member's peer review. Produce "
    "the single final answer the user will read. Resolve disagreements on the "
    "merits rather than by splitting the difference, correct errors the "
    "reviews identified, and keep the strongest material from any member. "
    "Write the answer directly to the user: do not mention the roundtable, "
    "the members, the reviews, or this process, and do not describe your own "
    "selection process. Where the members genuinely disagree on something "
    "consequential and the evidence does not settle it, say so plainly as "
    "part of the answer."
)

DIRECT_SYSTEM = (
    "You are a helpful, accurate assistant. Answer the user's request "
    "directly and completely."
)


def clamp(text: str, max_chars: int) -> str:
    """Trim `text` to `max_chars`, marking the cut so the model knows."""
    if max_chars <= 0 or len(text) <= max_chars:
        return text
    keep = max(max_chars - len(TRUNCATION_NOTE), 0)
    return text[:keep] + TRUNCATION_NOTE


def render_history(
    messages: Sequence[Mapping[str, str]],
    *,
    message_limit: int,
    char_limit: int,
) -> str:
    """Render prior turns as a transcript, newest turns preserved.

    The tail of the conversation matters most, so the limits drop the oldest
    turns first.
    """
    if not messages:
        return ""

    recent = list(messages)[-message_limit:]
    lines = [
        f"{'User' if m.get('role') == 'user' else 'Assistant'}: {m.get('content', '').strip()}"
        for m in recent
        if m.get("content", "").strip()
    ]
    if not lines:
        return ""

    transcript = "\n\n".join(lines)
    if len(transcript) > char_limit:
        # Keep the newest end of the transcript rather than the oldest.
        transcript = TRUNCATION_NOTE.strip() + "\n\n" + transcript[-char_limit:]
    return transcript


def _with_history(history: str, request: str) -> str:
    if not history:
        return f"The user's request:\n\n{request}"
    return (
        "Conversation so far:\n\n"
        f"{history}\n\n"
        "---\n\n"
        f"The user's new request:\n\n{request}"
    )


def build_independent_prompt(
    request: str,
    history: str = "",
    *,
    max_chars: int = 30000,
) -> str:
    return clamp(_with_history(history, request), max_chars)


def _format_answers(answers: Iterable[Mapping[str, str]]) -> str:
    blocks = []
    for answer in answers:
        blocks.append(
            f"### Answer from {answer['label']} ({answer['model']})\n\n{answer['text'].strip()}"
        )
    return "\n\n".join(blocks)


def build_review_prompt(
    request: str,
    answers: Sequence[Mapping[str, str]],
    reviewer_label: str,
    history: str = "",
    *,
    max_chars: int = 30000,
) -> str:
    prompt = (
        f"{_with_history(history, request)}\n\n"
        "---\n\n"
        "The roundtable produced these independent answers:\n\n"
        f"{_format_answers(answers)}\n\n"
        "---\n\n"
        f"You are {reviewer_label}. Review every answer above, including your "
        "own. For each one, identify concrete errors, omissions, and unsupported "
        "claims. Then state where the answers disagree and which position you "
        "judge correct, and list the strongest ideas that the final answer must "
        "keep."
    )
    return clamp(prompt, max_chars)


def build_chair_prompt(
    request: str,
    answers: Sequence[Mapping[str, str]],
    reviews: Sequence[Mapping[str, str]],
    history: str = "",
    *,
    max_chars: int = 30000,
) -> str:
    sections = [
        _with_history(history, request),
        "---",
        "Independent answers from the roundtable:",
        _format_answers(answers),
    ]

    if reviews:
        review_blocks = "\n\n".join(
            f"### Review by {review['label']}\n\n{review['text'].strip()}" for review in reviews
        )
        sections += ["---", "Peer reviews:", review_blocks]

    sections += [
        "---",
        "Write the final answer for the user now, using everything above.",
    ]
    return clamp("\n\n".join(sections), max_chars)


def build_direct_prompt(
    request: str,
    history: str = "",
    *,
    max_chars: int = 30000,
) -> str:
    return clamp(_with_history(history, request), max_chars)
