"""Prompt construction: truncation, history rendering, and phase prompts."""

from __future__ import annotations

from app.orchestration import prompts

ANSWERS = [
    {"label": "ChatGPT", "model": "gpt-5.6", "text": "Four."},
    {"label": "Claude", "model": "claude-sonnet-5", "text": "The answer is 4."},
]
REVIEWS = [
    {"label": "Gemini", "model": "gemini-3.7-flash", "text": "Both are correct."},
]


def test_clamp_marks_truncation():
    text = "x" * 500
    clamped = prompts.clamp(text, 100)
    assert len(clamped) == 100
    assert clamped.endswith(prompts.TRUNCATION_NOTE)


def test_clamp_leaves_short_text_alone():
    assert prompts.clamp("short", 100) == "short"


def test_render_history_keeps_the_newest_turns():
    messages = [{"role": "user", "content": f"message {i}"} for i in range(10)]
    rendered = prompts.render_history(messages, message_limit=3, char_limit=10_000)

    assert "message 9" in rendered
    assert "message 0" not in rendered
    assert rendered.count("User:") == 3


def test_render_history_labels_roles():
    rendered = prompts.render_history(
        [{"role": "user", "content": "hi"}, {"role": "assistant", "content": "hello"}],
        message_limit=10,
        char_limit=10_000,
    )
    assert "User: hi" in rendered
    assert "Assistant: hello" in rendered


def test_render_history_is_empty_for_no_messages():
    assert prompts.render_history([], message_limit=5, char_limit=100) == ""


def test_independent_prompt_includes_request_and_history():
    prompt = prompts.build_independent_prompt("What is 2+2?", "User: earlier question")
    assert "What is 2+2?" in prompt
    assert "earlier question" in prompt


def test_review_prompt_contains_every_answer_and_the_reviewer():
    prompt = prompts.build_review_prompt("What is 2+2?", ANSWERS, "Gemini")
    assert "ChatGPT" in prompt
    assert "Claude" in prompt
    assert "The answer is 4." in prompt
    assert "You are Gemini" in prompt


def test_chair_prompt_contains_answers_and_reviews():
    prompt = prompts.build_chair_prompt("What is 2+2?", ANSWERS, REVIEWS)
    assert "Independent answers" in prompt
    assert "Peer reviews" in prompt
    assert "Both are correct." in prompt


def test_chair_prompt_omits_the_review_section_when_there_are_none():
    prompt = prompts.build_chair_prompt("What is 2+2?", ANSWERS, [])
    assert "Peer reviews" not in prompt
    assert "Independent answers" in prompt


def test_prompts_respect_the_character_budget():
    long_request = "y" * 50_000
    for prompt in (
        prompts.build_independent_prompt(long_request, max_chars=1000),
        prompts.build_review_prompt(long_request, ANSWERS, "Claude", max_chars=1000),
        prompts.build_chair_prompt(long_request, ANSWERS, REVIEWS, max_chars=1000),
        prompts.build_direct_prompt(long_request, max_chars=1000),
    ):
        assert len(prompt) <= 1000


def test_no_prompt_requests_hidden_reasoning():
    """The app collaborates on visible output only."""
    banned = ("chain of thought", "chain-of-thought", "hidden reasoning", "internal reasoning")
    corpus = " ".join(
        [
            prompts.INDEPENDENT_SYSTEM,
            prompts.REVIEW_SYSTEM,
            prompts.CHAIR_SYSTEM,
            prompts.DIRECT_SYSTEM,
        ]
    ).lower()
    for phrase in banned:
        assert phrase not in corpus
