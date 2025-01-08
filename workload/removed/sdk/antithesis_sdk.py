import os
import json

"""
Quick wrapper around the fallback SDK
https://antithesis.com/docs/using_antithesis/sdk/fallback/assert.html
"""


class antithesis_fallback_sdk:
    def __init__(self):
        if "ANTITHESIS_OUTPUT_DIR" not in os.environ:
            raise Exception(
                "The environment variable ANTITHESIS_OUTPUT_DIR is not defined"
            )

        self.sdk_location = f"{os.environ['ANTITHESIS_OUTPUT_DIR']}/sdk.jsonl"
        self.assertion_types = ["always", "sometimes", "unreachable", "reachable"]

    def _get_assertion_evaulate(self) -> dict:
        always = {
            "hit": True,
            "must_hit": True,
            "condition": True,
        }

        sometimes = {
            "hit": True,
            "must_hit": True,
            "condition": True,
        }

        reachable = {
            "hit": True,
            "must_hit": True,
            "condition": True,
        }

        unreachable = {
            "hit": True,
            "must_hit": False,
            "condition": False,
        }

        return {
            "always": always,
            "sometimes": sometimes,
            "reachable": reachable,
            "unreachable": unreachable,
        }

    def _get_assertion_declare(self):
        """
        table of metadata for different assertions
        """
        always = {
            "hit": False,
            "must_hit": True,
            "assert_type": "always",
            "display_type": "Always",
            "condition": False,
            "details": None,
        }

        sometimes = {
            "hit": False,
            "must_hit": True,
            "assert_type": "sometimes",
            "display_type": "Sometimes",
            "condition": False,
            "details": None,
        }

        unreachable = {
            "hit": False,
            "must_hit": False,
            "assert_type": "reachability",
            "display_type": "Unreachable",
            "condition": False,
            "details": None,
        }

        reachable = {
            "hit": False,
            "must_hit": True,
            "assert_type": "reachability",
            "display_type": "Reachable",
            "condition": False,
            "details": None,
        }

        return {
            "always": always,
            "sometimes": sometimes,
            "unreachable": unreachable,
            "reachable": reachable,
        }

    def _declare(self, assert_type: str, id: str, message: str) -> dict:
        """
        Make the assertion declaration
        """
        if assert_type not in self.assertion_types:
            raise Exception(
                f"The assertion type must be one of {', '.join(self.assertion_types)}"
            )

        assertions_declare = self._get_assertion_declare()
        payload = {"antithesis_assert": assertions_declare[assert_type]}

        # Stubbing these out for now
        location = {
            "class": "",
            "function": "",
            "file": "",
            "begin_line": 0,
            "begin_column": 0,
        }

        payload["antithesis_assert"]["location"] = location
        payload["antithesis_assert"]["id"] = id
        payload["antithesis_assert"]["message"] = message

        return payload

    def declare(self, assert_type: str, id: str, message: str) -> None:
        payload = self._declare(assert_type, id, message)
        self._write_to_json(payload)

    def do_assert(
        self,
        assert_type: str,
        id: str,
        message: str,
        condition: bool,
        details: dict | None = None,
    ) -> None:
        """
        Write the assert evaulation to the event sink
        """

        # Build the base payload
        payload = self._declare(assert_type, id, message)

        # Change the values based on evaulation
        assert_eval = self._get_assertion_evaulate()[assert_type]

        antithesis_assert = payload["antithesis_assert"]
        antithesis_assert.update(assert_eval)

        payload["antithesis_assert"] = antithesis_assert

        payload["antithesis_assert"]["condition"] = condition

        if bool(details):
            payload["antithesis_assert"]["details"] = details

        self._write_to_json(payload)

    def always(
        self,
        declare: bool,
        id: str,
        message: str,
        condition: bool = False,
        details: dict | None = None,
    ):
        if declare:
            self.declare("always", id, message)
        else:
            self.do_assert("always", id, message, condition, details)

    def sometimes(
        self,
        declare: bool,
        id: str,
        message: str,
        condition: bool = False,
        details: dict | None = None,
    ):
        if declare:
            self.declare("sometimes", id, message)
        else:
            self.do_assert("sometimes", id, message, condition, details)

    def reachable(
        self,
        declare: bool,
        id: str,
        message: str,
        condition: bool = False,
        details: dict | None = None,
    ):
        if declare:
            self.declare("reachable", id, message)
        else:
            self.do_assert("reachable", id, message, condition, details)

    def unreachable(
        self,
        declare: bool,
        id: str,
        message: str,
        condition: bool = False,
        details: dict | None = None,
    ):
        if declare:
            self.declare("unreachable", id, message)
        else:
            self.do_assert("unreachable", id, message, condition, details)

    def _write_to_json(self, assertion_obj: dict) -> None:
        """
        Appending the data to json
        """
        with open(self.sdk_location, "a") as fh:
            fh.write(f"{json.dumps(assertion_obj)}\n")

    def get_random_int(self, bytes=1) -> int:
        random_bytes = os.urandom(bytes)
        return int.from_bytes(random_bytes, byteorder="big")

    def setup_complete(self, details: dict = {}) -> None:
        message = {"antithesis_setup": {"status": "complete", "details": details}}

        self._write_to_json(message)
