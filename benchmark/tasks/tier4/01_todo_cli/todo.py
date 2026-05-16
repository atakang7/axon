#!/usr/bin/env python3
"""TODO CLI - A simple command-line todo list manager with JSON persistence."""

import sys
import argparse
import json
import os
from typing import Dict, List, Optional


def load_todos(filename: str = "todos.json") -> tuple[List[Dict], int]:
    """Load todos from JSON file. Return (todos_list, next_id).
    If file doesn't exist or is empty, return empty list and next_id=1."""
    if not os.path.exists(filename):
        return [], 1

    try:
        with open(filename, "r") as f:
            data = json.load(f)
            if isinstance(data, dict) and "todos" in data and "next_id" in data:
                todos = data["todos"]
                next_id = data["next_id"]
                if isinstance(todos, list):
                    return todos, next_id
                else:
                    return [], next_id
            elif isinstance(data, list):
                # Legacy format - no next_id stored
                if data:
                    max_id = max(todo.get("id", 0) for todo in data)
                    next_id = max_id + 1
                else:
                    next_id = 1
                return data, next_id
            else:
                return [], 1
    except (json.JSONDecodeError, IOError, ValueError, KeyError):
        return [], 1


def save_todos(todos: List[Dict], next_id: int, filename: str = "todos.json"):
    """Save todos and next_id to JSON file with indentation."""
    data = {"todos": todos, "next_id": next_id}
    with open(filename, "w") as f:
        json.dump(data, f, indent=2)


def add_todo(todos: List[Dict], next_id: int, text: str) -> tuple[int, int]:
    """Add a new todo and return (new_id, updated_next_id)."""
    new_id = next_id
    todo = {"id": new_id, "text": text, "done": False}
    todos.append(todo)
    return new_id, next_id + 1


def list_todos(todos: List[Dict]):
    """Print todos in the required format."""
    for todo in todos:
        status = "x" if todo.get("done", False) else " "
        print(f"{todo['id']} [{status}] {todo['text']}")


def mark_done(todos: List[Dict], todo_id: int) -> bool:
    """Mark a todo as done. Return True if found, False otherwise."""
    for todo in todos:
        if todo.get("id") == todo_id:
            todo["done"] = True
            return True
    return False


def remove_todo(todos: List[Dict], todo_id: int) -> bool:
    """Remove a todo by ID. Return True if found, False otherwise."""
    for i, todo in enumerate(todos):
        if todo.get("id") == todo_id:
            todos.pop(i)
            return True
    return False


def clear_done_todos(todos: List[Dict]) -> int:
    """Remove all done todos. Return number removed."""
    initial_len = len(todos)
    todos[:] = [todo for todo in todos if not todo.get("done", False)]
    return initial_len - len(todos)


def main():
    parser = argparse.ArgumentParser(description="Manage a todo list")
    subparsers = parser.add_subparsers(dest="command", required=True)

    # Add command
    add_parser = subparsers.add_parser("add", help="Add a new todo")
    add_parser.add_argument("text", help="Todo text (in quotes if contains spaces)")

    # List command
    subparsers.add_parser("list", help="List all todos")

    # Done command
    done_parser = subparsers.add_parser("done", help="Mark a todo as done")
    done_parser.add_argument("id", type=int, help="Todo ID")

    # Remove command
    rm_parser = subparsers.add_parser("rm", help="Remove a todo")
    rm_parser.add_argument("id", type=int, help="Todo ID")

    # Clear command
    subparsers.add_parser("clear", help="Remove all done todos")

    args = parser.parse_args()

    # Load todos and next_id counter
    todos, next_id = load_todos()

    # Dispatch command
    if args.command == "add":
        new_id, next_id = add_todo(todos, next_id, args.text)
        save_todos(todos, next_id)
        print(new_id)

    elif args.command == "list":
        list_todos(todos)

    elif args.command == "done":
        if mark_done(todos, args.id):
            save_todos(todos, next_id)
        else:
            print(f"Error: todo with id {args.id} not found", file=sys.stderr)
            sys.exit(1)

    elif args.command == "rm":
        if remove_todo(todos, args.id):
            save_todos(todos, next_id)
        else:
            print(f"Error: todo with id {args.id} not found", file=sys.stderr)
            sys.exit(1)

    elif args.command == "clear":
        removed = clear_done_todos(todos)
        save_todos(todos, next_id)

    else:
        parser.print_help()
        sys.exit(1)


if __name__ == "__main__":
    main()
