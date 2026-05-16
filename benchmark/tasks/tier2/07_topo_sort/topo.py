#!/usr/bin/env python3
import json
import sys
from collections import deque
import heapq


def topological_sort(graph):
    """
    Perform topological sort using Kahn's algorithm with alphabetical tie-breaking.

    Args:
        graph: dict mapping node -> list of successor nodes

    Returns:
        list of nodes in topological order
    """
    # Calculate in-degree for each node
    in_degree = {node: 0 for node in graph}
    for node, successors in graph.items():
        for succ in successors:
            in_degree[succ] = in_degree.get(succ, 0) + 1

    # Initialize nodes with zero in-degree
    # Use a min-heap for alphabetical tie-breaking
    zero_degree = [node for node, deg in in_degree.items() if deg == 0]
    heapq.heapify(zero_degree)

    result = []

    while zero_degree:
        # Pop the smallest node alphabetically
        node = heapq.heappop(zero_degree)
        result.append(node)

        # Reduce in-degree of successors
        for succ in graph.get(node, []):
            in_degree[succ] -= 1
            if in_degree[succ] == 0:
                heapq.heappush(zero_degree, succ)

    # Check for cycles (if result length < total nodes)
    if len(result) != len(in_degree):
        raise ValueError("Graph contains a cycle")

    return result


def main():
    # Read graph from graph.json
    try:
        with open("graph.json", "r") as f:
            graph = json.load(f)
    except FileNotFoundError:
        print("Error: graph.json not found", file=sys.stderr)
        sys.exit(1)
    except json.JSONDecodeError:
        print("Error: Invalid JSON in graph.json", file=sys.stderr)
        sys.exit(1)

    # Perform topological sort
    try:
        order = topological_sort(graph)
    except ValueError as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)

    # Print result, one node per line
    for node in order:
        print(node)


if __name__ == "__main__":
    main()
