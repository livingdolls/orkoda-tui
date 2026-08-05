from pathlib import Path

path = Path("scripts/apply_review_ui.py")
text = path.read_text()
old = '''replace_once(board_detail, 'import { type ReviewIssue, type ReviewRun, type ReviewSeverity, listReviewIssues, listReviews } from "./reviews"\\n', 'import { type ReviewIssue, type ReviewRun, type ReviewSeverity, listReviewIssues, listReviews } from "./reviews"\\nimport { compareReviewIssues } from "./review-board-model"\\n')'''
new = '''replace_once(
    board_detail,
    ''' + "'''" + '''} from "./reviews"
''' + "'''" + ''',
    ''' + "'''" + '''} from "./reviews"
import { compareReviewIssues } from "./review-board-model"
''' + "'''" + ''',
)'''
if old not in text:
    raise SystemExit("review import patch marker not found")
path.write_text(text.replace(old, new, 1))
