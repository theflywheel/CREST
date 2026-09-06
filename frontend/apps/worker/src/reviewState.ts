const PENDING_REVIEW_KEY = "crest.worker.pending-review";

export function rememberReview(path: string, search: string): void {
  try {
    sessionStorage.setItem(PENDING_REVIEW_KEY, path + search);
  } catch {
    // A blocked session store only loses redirect continuity.
  }
}

export function takePendingReview(): string | null {
  try {
    const path = sessionStorage.getItem(PENDING_REVIEW_KEY);
    sessionStorage.removeItem(PENDING_REVIEW_KEY);
    return path;
  } catch {
    return null;
  }
}
