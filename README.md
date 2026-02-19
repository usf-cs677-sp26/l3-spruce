# file-transfer

Changes made in the server file:-
  1. Added error handling
  2. Extract the actual filename using strings.Split on /, preventing client from writing files to arbitrary paths
