from pathlib import Path
import re

for name in [
    "internal/config/llm.go",
    "internal/config/llm_multi_provider_test.go",
    "internal/agentconfig/separation_test.go",
]:
    path = Path(name)
    text = path.read_text()
    text = re.sub(
        r"^(?P<indent>(?:\\t)+)",
        lambda match: "\t" * (len(match.group("indent")) // 2),
        text,
        flags=re.MULTILINE,
    )
    path.write_text(text)
