from pathlib import Path

path = Path("scripts/harden_executor_startup_recovery.py")
text = path.read_text()
text = text.replace("j.status = 'DEAD' '''", "j.status = 'DEAD''''")
text = text.replace("j.status IN ('DEAD', 'COMPLETED') '''", "j.status IN ('DEAD', 'COMPLETED')'''")
path.write_text(text)
print("hardening patch patterns normalized")
