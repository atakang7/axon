import os
import sys
import json


def load_config():
    """Load configuration merging defaults.yaml, local.yaml, and APP__ env vars."""
    # First load defaults.yaml
    config = _load_yaml("defaults.yaml")

    # Merge local.yaml if exists
    if os.path.exists("local.yaml"):
        local_config = _load_yaml("local.yaml")
        config = _deep_merge(config, local_config)

    # Apply environment overrides
    for key, value in os.environ.items():
        if key.startswith("APP__"):
            path = key[5:]  # Remove APP__
            path_parts = path.lower().split("__")  # Normalize to lowercase
            _set_nested(config, path_parts, _convert_env_value(value))

    return config


def _load_yaml(path):
    """Load YAML file using pyyaml if available, else simple parser."""
    try:
        import yaml

        with open(path, "r") as f:
            return yaml.safe_load(f) or {}
    except ImportError:
        # Simple parser for flat/nested YAML like defaults.yaml
        return _simple_yaml_load(path)


def _simple_yaml_load(path):
    """Parse simple YAML with flat keys and single-level nesting."""
    with open(path, "r") as f:
        lines = f.readlines()

    result = {}
    current_section = None
    indent_level = 0

    for line in lines:
        stripped = line.rstrip("\n")
        if not stripped.strip() or stripped.strip().startswith("#"):
            continue

        # Count leading spaces for indentation
        leading = len(stripped) - len(stripped.lstrip())

        if ":" in stripped:
            key, value = stripped.split(":", 1)
            key = key.strip()
            value = value.strip()

            if leading == 0:
                # Top-level key
                current_section = None
                if value == "":
                    # Start a new section
                    current_section = key
                    if key not in result:
                        result[key] = {}
                else:
                    result[key] = _parse_yaml_value(value)
            else:
                # Indented line - belongs to current section
                if current_section:
                    result[current_section][key] = _parse_yaml_value(value)

    return result


def _parse_yaml_value(value_str):
    """Parse YAML value string into Python type."""
    if not value_str:
        return None
    if value_str.lower() == "true":
        return True
    if value_str.lower() == "false":
        return False
    if value_str.isdigit():
        return int(value_str)
    try:
        # Try float for completeness
        return float(value_str)
    except ValueError:
        # Remove quotes if present
        if (value_str.startswith('"') and value_str.endswith('"')) or (
            value_str.startswith("'") and value_str.endswith("'")
        ):
            return value_str[1:-1]
        return value_str


def _deep_merge(base, override):
    """Recursively merge override dict into base dict."""
    result = base.copy()
    for key, value in override.items():
        if key in result and isinstance(result[key], dict) and isinstance(value, dict):
            result[key] = _deep_merge(result[key], value)
        else:
            result[key] = value
    return result


def _convert_env_value(value):
    """Convert environment variable string to appropriate type."""
    if value.lower() == "true":
        return True
    if value.lower() == "false":
        return False
    if value.isdigit():
        return int(value)
    # Try float
    try:
        if "." in value and all(part.isdigit() for part in value.split(".", 1)):
            return float(value)
    except ValueError:
        pass
    return value


def _set_nested(config, path_parts, value):
    """Set nested key in config dict, creating intermediate dicts if needed."""
    current = config
    for part in path_parts[:-1]:
        if part not in current:
            current[part] = {}
        elif not isinstance(current[part], dict):
            # Override scalar with dict
            current[part] = {}
        current = current[part]
    current[path_parts[-1]] = value


if __name__ == "__main__":
    # For debugging
    print(json.dumps(load_config(), sort_keys=True))
