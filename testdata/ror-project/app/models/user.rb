# Sample ActiveRecord model
class User < ApplicationRecord
  has_many :posts
  validates :name, presence: true
  validates :email, presence: true, uniqueness: true
end
