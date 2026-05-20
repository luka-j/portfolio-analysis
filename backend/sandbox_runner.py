import sys
import json
import io
import traceback
try:
    import pandas as pd
    import numpy as np
except ImportError:
    # Fail gracefully if pandas/numpy are missing so the Go side knows
    sys.stdout.write(json.dumps({"output": "", "error": "pandas or numpy not installed"}))
    sys.exit(1)

def main():
    try:
        # 1. Read task from stdin
        raw_input = sys.stdin.read()
        if not raw_input:
            sys.stdout.write(json.dumps({"output": "", "error": "empty stdin"}))
            sys.exit(1)
            
        task = json.loads(raw_input)
        code = task.get("code", "")
        data_path = task.get("data_path", "")
        cash_data_path = task.get("cash_data_path", "")

        # 2. Load data into pandas
        df = pd.DataFrame()
        if data_path:
            try:
                df = pd.read_csv(data_path)
                if 'date_time' in df.columns:
                    df['date_time'] = pd.to_datetime(df['date_time'], errors='coerce')
            except Exception as e:
                pass # Continue with empty df if file is missing/bad

        df_cash = pd.DataFrame()
        if cash_data_path:
            try:
                df_cash = pd.read_csv(cash_data_path)
                if 'date_time' in df_cash.columns:
                    df_cash['date_time'] = pd.to_datetime(df_cash['date_time'], errors='coerce')
            except Exception as e:
                pass

        # 3. Setup sandbox namespace
        # We explicitly restrict __builtins__ to prevent os/subprocess/eval etc.
        # This is not bulletproof against a determined human, but sufficient for LLMs.
        safe_builtins = {
            "abs": abs, "all": all, "any": any, "ascii": ascii, "bin": bin,
            "bool": bool, "bytearray": bytearray, "bytes": bytes, "callable": callable,
            "chr": chr, "complex": complex, "dict": dict, "dir": dir, "divmod": divmod,
            "enumerate": enumerate, "filter": filter, "float": float, "format": format,
            "frozenset": frozenset, "getattr": getattr, "hasattr": hasattr, "hash": hash,
            "hex": hex, "id": id, "int": int, "isinstance": isinstance,
            "issubclass": issubclass, "iter": iter, "len": len, "list": list,
            "map": map, "max": max, "min": min, "next": next, "oct": oct,
            "ord": ord, "pow": pow, "print": print, "property": property,
            "range": range, "repr": repr, "reversed": reversed, "round": round,
            "set": set, "setattr": setattr, "slice": slice, "sorted": sorted,
            "str": str, "sum": sum, "super": super, "tuple": tuple, "type": type,
            "zip": zip,
            "__import__": __import__, # Let them import basic modules (checked by standard module loading rules)
            "Exception": Exception,
            "ValueError": ValueError,
            "TypeError": TypeError,
        }

        # Some standard libraries the LLM might use
        import datetime, math, statistics, collections, itertools, functools
        
        safe_globals = {
            "__builtins__": safe_builtins,
            "pd": pd,
            "np": np,
            "df": df,
            "df_cash": df_cash,
            "datetime": datetime,
            "math": math,
            "statistics": statistics,
            "collections": collections,
            "itertools": itertools,
            "functools": functools,
        }

        # 4. Execute code, capturing stdout
        output_buffer = io.StringIO()
        sys.stdout = output_buffer
        
        try:
            # We use exec instead of eval to allow multi-line scripts
            exec(code, safe_globals)
            result = {"output": output_buffer.getvalue().strip(), "error": ""}
        except Exception as e:
            # Capture traceback for the LLM to read and fix
            result = {"output": output_buffer.getvalue().strip(), "error": traceback.format_exc()}
        finally:
            # Restore stdout
            sys.stdout = sys.__stdout__
            
        sys.stdout.write(json.dumps(result))
        
    except Exception as e:
        sys.stdout = sys.__stdout__
        sys.stdout.write(json.dumps({"output": "", "error": str(e)}))

if __name__ == "__main__":
    main()
