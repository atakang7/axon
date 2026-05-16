#!/usr/bin/env python3
import sys
import os
import subprocess

class MiniShell:
    def __init__(self):
        self.variables = {}
        self.should_exit = False
        self.exit_code = 0
    
    def parse_line(self, line):
        """Parse a line, handling comments and trimming."""
        line = line.strip()
        # Skip empty lines and comments
        if not line or line.startswith('#'):
            return None
        return line
    
    def split_command(self, line):
        """Split command into parts, handling basic tokenization."""
        # Simple split on whitespace for now
        # TODO: handle quotes and special characters
        return line.split()
    
    def run(self, script_path):
def run(self, script_path):
        """Run the script from given path."""
        try:
            with open(script_path, 'r') as f:
                for line_num, line in enumerate(f, 1):
                    parsed = self.parse_line(line)
                    if parsed is None:
                        continue
                    
                    parts = self.split_command(parsed)
                    if not parts:
                        continue
                    
                    cmd = parts[0]
                    args = parts[1:]
                    
                    # Handle builtins
                    if cmd == 'cd':
                        self.builtin_cd(args)
                    elif cmd == 'set':
                        self.builtin_set(' '.join(args))
                    elif cmd == 'echo':
                        self.builtin_echo(args)
                    elif cmd == 'exit':
                        self.builtin_exit(args)
                        if self.should_exit:
                            break
                    else:
                        # External command
                        self.run_external(cmd, args)
                    
                    if self.should_exit:
                        break
                        
        except FileNotFoundError:
            print(f"Error: Script file '{script_path}' not found", file=sys.stderr)
            sys.exit(1)
        except Exception as e:
            print(f"Error: {e}", file=sys.stderr)
            sys.exit(1)
    
    def builtin_cd(self, args):
        """Change directory builtin."""
        if not args:
            # cd with no args should probably go to home, but spec says <dir> required
            # For simplicity, we'll handle error case
            print("cd: missing directory", file=sys.stderr)
            return
        
        dir_path = args[0]
        try:
            os.chdir(dir_path)
        except FileNotFoundError:
            print(f"cd: {dir_path}: No such file or directory", file=sys.stderr)
        except PermissionError:
            print(f"cd: {dir_path}: Permission denied", file=sys.stderr)
        except Exception as e:
            print(f"cd: {e}", file=sys.stderr)
    
    def builtin_set(self, arg_string):
        """Set variable builtin. Format: set NAME=VALUE"""
        if '=' not in arg_string:
            # Invalid format
            return
        
        name, value = arg_string.split('=', 1)
        name = name.strip()
        value = value.strip()
        self.variables[name] = value
    
    def builtin_echo(self, args):
        """Echo builtin. Supports $NAME variable substitution."""
        if not args:
            print()
            return
        
        # Process each argument: strip surrounding quotes and handle variable substitution
        output_parts = []
        for arg in args:
            # Strip surrounding single or double quotes
            if (arg.startswith('"') and arg.endswith('"')) or (arg.startswith("'") and arg.endswith("'")):
                arg = arg[1:-1]
            
            # Handle variable substitution
            if arg.startswith('$'):
                var_name = arg[1:]
                value = self.variables.get(var_name, '')
                output_parts.append(value)
            else:
                # Check if argument contains $variable pattern within it
                # Simple implementation: look for $VAR pattern
                result = arg
                # Replace $VAR with variable value
                # This is a simple implementation that doesn't handle complex cases
                for var_name, var_value in self.variables.items():
                    if f'${var_name}' in result:
                        result = result.replace(f'${var_name}', var_value)
                output_parts.append(result)
        
        print(' '.join(output_parts))
    
    def builtin_exit(self, args):
        """Exit builtin. exit [code]"""
def run_external(self, cmd, args):
        """Run external command via subprocess.run, inheriting stdout/stderr."""
        try:
            # Build command list
            command = [cmd] + args
            # Run command, inheriting stdout/stderr
            result = subprocess.run(command, check=False)
            # According to spec, failing external command does NOT abort the script
            # We just continue regardless of exit code
        except FileNotFoundError:
            print(f"{cmd}: command not found", file=sys.stderr)
        except Exception as e:
            print(f"Error running {cmd}: {e}", file=sys.stderr)
        self.should_exit = True
    
    def run_external(self, cmd, args):
        """Run external command."""
        # TODO: implement with subprocess.run
        pass

def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <script>", file=sys.stderr)
        sys.exit(1)
    
    shell = MiniShell()
    shell.run(sys.argv[1])
    sys.exit(shell.exit_code)

if __name__ == '__main__':
    main()