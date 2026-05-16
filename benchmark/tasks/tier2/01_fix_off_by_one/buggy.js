// Returns the last `n` elements of arr. Currently buggy.
function lastN(arr, n) {
  if (n <= 0) return [];
  if (n > arr.length) return arr.slice(); // return full copy
  const out = [];
  for (let i = arr.length - n; i < arr.length; i++) {
    out.push(arr[i]);
  }
  return out;
}

console.log(JSON.stringify(lastN([1, 2, 3, 4, 5], 2))); // expect [4,5]
console.log(JSON.stringify(lastN([1, 2, 3], 10))); // expect [1,2,3]
console.log(JSON.stringify(lastN([1, 2, 3], 0))); // expect []
console.log(JSON.stringify(lastN([], 3))); // expect []

module.exports = { lastN };
