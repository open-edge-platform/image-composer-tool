#!/bin/bash

# Comprehensive fuzz test runner for all fuzz tests in the image-composer project
set -e

echo "Running All Fuzz Tests"
echo "======================"

# Test duration (can be overridden with environment variable)
FUZZ_TIME=${FUZZ_TIME:-30s}

echo "Fuzz test duration: ${FUZZ_TIME}"
echo ""

# Dynamically find the project root directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}" && git rev-parse --show-toplevel 2>/dev/null || echo "${SCRIPT_DIR}")"

echo "Running from: ${PROJECT_ROOT}"
cd "${PROJECT_ROOT}"

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to run a single fuzz test (for packages)
run_package_fuzz_test() {
    local package=$1
    local test_name=$2
    local test_start=$(date +%s)
    local log_file="/tmp/fuzz_${test_name}_${RANDOM}.log"
    echo -e "${YELLOW}Testing ${test_name} in ${package}...${NC}"
    
    if go test -run='^$' -fuzz="${test_name}" -fuzztime="${FUZZ_TIME}" "./${package}" 2>&1 | tee "${log_file}"; then
        local test_end=$(date +%s)
        local duration=$((test_end - test_start))
        local log_content=$(cat "${log_file}" | base64 -w 0)
        echo -e "${GREEN}✅ ${test_name} PASSED!${NC}"
        TEST_RESULTS+=("PASS|${package}|${test_name}|${duration}s")
        TEST_LOGS+=("${package}|${test_name}|${log_content}")
        rm -f "${log_file}"
        return 0
    else
        local test_end=$(date +%s)
        local duration=$((test_end - test_start))
        local log_content=$(cat "${log_file}" | base64 -w 0)
        echo -e "${RED}❌ ${test_name} FAILED!${NC}"
        TEST_RESULTS+=("FAIL|${package}|${test_name}|${duration}s")
        TEST_LOGS+=("${package}|${test_name}|${log_content}")
        rm -f "${log_file}"
        return 1
    fi
}

# Function to run a single fuzz test (for main command package)
run_main_fuzz_test() {
    local test_name=$1
    local test_start=$(date +%s)
    local log_file="/tmp/fuzz_${test_name}_${RANDOM}.log"
    echo -e "${YELLOW}Testing ${test_name} function...${NC}"
    
    if go test -run='^$' -fuzz="${test_name}" -fuzztime="${FUZZ_TIME}" ./cmd/image-composer-tool 2>&1 | tee "${log_file}"; then
        local test_end=$(date +%s)
        local duration=$((test_end - test_start))
        local log_content=$(cat "${log_file}" | base64 -w 0)
        echo -e "${GREEN}✅ ${test_name} PASSED!${NC}"
        TEST_RESULTS+=("PASS|cmd/image-composer-tool|${test_name}|${duration}s")
        TEST_LOGS+=("cmd/image-composer-tool|${test_name}|${log_content}")
        rm -f "${log_file}"
        return 0
    else
        local test_end=$(date +%s)
        local duration=$((test_end - test_start))
        local log_content=$(cat "${log_file}" | base64 -w 0)
        echo -e "${RED}❌ ${test_name} FAILED!${NC}"
        TEST_RESULTS+=("FAIL|cmd/image-composer-tool|${test_name}|${duration}s")
        TEST_LOGS+=("cmd/image-composer-tool|${test_name}|${log_content}")
        rm -f "${log_file}"
        return 1
    fi
}

# Track test results
TOTAL_TESTS=0
FAILED_TESTS=0
TEST_RESULTS=()
TEST_LOGS=()
START_TIME=$(date +%s)
REPORT_FILE="fuzz_test_report_$(date +%Y%m%d_%H%M%S).md"

echo -e "${BLUE}=== MAIN.GO FUZZ TESTS ===${NC}"

# Test 1: createRootCommand function
TOTAL_TESTS=$((TOTAL_TESTS + 1))
if ! run_main_fuzz_test "FuzzCreateRootCommand"; then
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

echo ""

# Test 2: Command line argument handling
TOTAL_TESTS=$((TOTAL_TESTS + 1))
if ! run_main_fuzz_test "FuzzCommandLineArgs"; then
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

echo ""
echo -e "${BLUE}=== CONFIG PACKAGE FUZZ TESTS ===${NC}"

# Test 3: Config template loading
TOTAL_TESTS=$((TOTAL_TESTS + 1))
if ! run_package_fuzz_test "internal/config" "FuzzLoadTemplate"; then
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

echo ""

# Test 4: YAML parsing
TOTAL_TESTS=$((TOTAL_TESTS + 1))
if ! run_package_fuzz_test "internal/config" "FuzzParseYAMLTemplate"; then
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

echo ""
echo -e "${BLUE}=== VALIDATION PACKAGE FUZZ TESTS ===${NC}"

# Test 6: Image template validation
TOTAL_TESTS=$((TOTAL_TESTS + 1))
if ! run_package_fuzz_test "internal/config/validate" "FuzzValidateImageTemplateJSON"; then
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

echo ""

# Test 7: User template validation
TOTAL_TESTS=$((TOTAL_TESTS + 1))
if ! run_package_fuzz_test "internal/config/validate" "FuzzValidateUserTemplateJSON"; then
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

echo ""

# Test 8: Config validation
TOTAL_TESTS=$((TOTAL_TESTS + 1))
if ! run_package_fuzz_test "internal/config/validate" "FuzzValidateConfigJSON"; then
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

echo ""
echo -e "${BLUE}=== MANIFEST PACKAGE FUZZ TESTS ===${NC}"

# Test 9: Manifest writing
TOTAL_TESTS=$((TOTAL_TESTS + 1))
if ! run_package_fuzz_test "internal/config/manifest" "FuzzWriteManifestToFile"; then
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

echo ""

# Test 10: SPDX writing
TOTAL_TESTS=$((TOTAL_TESTS + 1))
if ! run_package_fuzz_test "internal/config/manifest" "FuzzWriteSPDXToFile"; then
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

echo ""

# Test 11: Document namespace generation
TOTAL_TESTS=$((TOTAL_TESTS + 1))
if ! run_package_fuzz_test "internal/config/manifest" "FuzzGenerateDocumentNamespace"; then
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Function to generate detailed fuzz test report
generate_report() {
    local end_time=$(date +%s)
    local total_duration=$((end_time - START_TIME))
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    
    echo "Generating fuzz test report: ${REPORT_FILE}"
    
    cat > "${REPORT_FILE}" << EOF
# Fuzz Test Report

This report contains the complete output logs from each fuzz test execution.

**Generated:** ${timestamp}  
**Duration:** ${total_duration} seconds  
**Test Time per Function:** ${FUZZ_TIME}  
**Total Functions Tested:** ${TOTAL_TESTS}  

## Executive Summary

- **✅ Passed:** $((TOTAL_TESTS - FAILED_TESTS)) tests
- **❌ Failed:** ${FAILED_TESTS} tests
- **📊 Success Rate:** $(( (TOTAL_TESTS - FAILED_TESTS) * 100 / TOTAL_TESTS ))%
- **⏱️ Total Execution Time:** ${total_duration} seconds

---

EOF

    # Add individual test sections with logs
    for log_entry in "${TEST_LOGS[@]}"; do
        # Split on first two pipes only
        local package=$(echo "$log_entry" | cut -d'|' -f1)
        local test_name=$(echo "$log_entry" | cut -d'|' -f2)
        local log_content_b64=$(echo "$log_entry" | cut -d'|' -f3-)
        local log_content=$(echo "$log_content_b64" | base64 -d)
        
        # Determine status for this test
        local test_status="UNKNOWN"
        for result in "${TEST_RESULTS[@]}"; do
            IFS='|' read -r status result_package result_test_name duration <<< "$result"
            if [[ "$result_package" == "$package" && "$result_test_name" == "$test_name" ]]; then
                test_status="$status"
                break
            fi
        done
        
        cat >> "${REPORT_FILE}" << EOF
## ${test_name} (${package})

**Status:** ${test_status}  
**Package:** ${package}  

### Complete Test Output

\`\`\`
${log_content}
\`\`\`

---

EOF
    done

    # Add failure analysis if any tests failed
    if [ $FAILED_TESTS -gt 0 ]; then
        cat >> "${REPORT_FILE}" << EOF
## Failed Tests Analysis

The following tests failed and require investigation:

EOF
        for result in "${TEST_RESULTS[@]}"; do
            IFS='|' read -r status package test_name duration <<< "$result"
            if [[ "$status" == "FAIL" ]]; then
                cat >> "${REPORT_FILE}" << EOF
### ${test_name} (${package})
- **Duration:** ${duration}
- **Package:** ${package}
- **Reproducer:** \`go test -run=${test_name} -v ./${package}\`
- **Failure Data:** Check \`testdata/fuzz/${test_name}/\` directory

EOF
            fi
        done
    fi

    echo -e "${GREEN}📄 Detailed report saved to: ${REPORT_FILE}${NC}"
}

echo ""
echo "============================================"
echo "COMPREHENSIVE FUZZ TEST SUMMARY"
echo "============================================"
echo "Total tests: $TOTAL_TESTS"
echo "Passed: $((TOTAL_TESTS - FAILED_TESTS))"
echo "Failed: $FAILED_TESTS"

# Generate detailed report
generate_report

if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}All fuzz tests passed!${NC}"
    echo ""
    echo "The image-composer application handles various input combinations without crashing."
    echo "This helps ensure robust operation against malformed inputs and edge cases."
    echo ""
    echo "Key areas tested:"
    echo "- Main command creation and argument parsing"
    echo "- Configuration loading and parsing (YAML/JSON)"
    echo "- Schema validation and template processing"
    echo "- Manifest and SPDX document generation"
    echo ""
    echo -e "${BLUE}📊 Detailed report available in: ${REPORT_FILE}${NC}"
    exit 0
else
    echo -e "${RED}$FAILED_TESTS fuzz test(s) failed.${NC}"
    echo ""
    echo "Check the output above for details about what input caused the failure."
    echo "Failing inputs are saved in testdata/fuzz/FuzzFunctionName/"
    echo ""
    echo "To debug a specific failure:"
    echo "1. Check the testdata/fuzz/ directory for saved failing inputs"
    echo "2. Run the specific test with -v flag for verbose output"
    echo "3. Use go test -run=FuzzFunctionName to reproduce the failure"
    echo ""
    echo "Examples:"
    echo "  go test -run=FuzzCreateRootCommand -v ./cmd/image-composer-tool"
    echo "  go test -run=FuzzValidateAgainstSchema -v ./internal/config/validate"
    echo ""
    echo -e "${BLUE}📊 Detailed failure analysis available in: ${REPORT_FILE}${NC}"
    exit 1
fi
