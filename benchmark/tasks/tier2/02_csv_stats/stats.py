#!/usr/bin/env python3
import csv
import statistics


def main():
    scores = []
    name_score_pairs = []

    with open("data.csv", "r") as f:
        reader = csv.DictReader(f)
        for row in reader:
            score = float(row["score"])
            scores.append(score)
            name_score_pairs.append((row["name"], score))

    count = len(scores)
    mean = statistics.mean(scores) if scores else 0
    median = statistics.median(scores) if scores else 0
    stdev = statistics.stdev(scores) if len(scores) > 1 else 0

    # Sort by score descending, then take top 3 names
    name_score_pairs.sort(key=lambda x: x[1], reverse=True)
    top3_names = [name for name, _ in name_score_pairs[:3]]

    print(f"count: {count}")
    print(f"mean: {mean:.2f}")
    print(f"median: {median:.2f}")
    print(f"stdev: {stdev:.4f}")
    print(f"top3: {','.join(top3_names)}")


if __name__ == "__main__":
    main()
